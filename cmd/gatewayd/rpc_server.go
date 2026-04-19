// Package main contains the RPC server for gatewayd.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/rpc"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
	"gopkg.in/yaml.v3"
)

// RPCServer handles RPC calls from controld.
type RPCServer struct {
	daemon *Daemon
	server *rpc.Server
}

// NewRPCServer creates a new RPC server.
func NewRPCServer(daemon *Daemon) *RPCServer {
	s := &RPCServer{
		daemon: daemon,
		server: rpc.NewServer(),
	}
	s.server.Register(&GatewayControlRPC{daemon: daemon})
	return s
}

// ServeConn serves a single RPC connection.
func (s *RPCServer) ServeConn(conn contracts.Conn) {
	s.server.ServeConn(&connAdapter{conn: conn})
}

// GatewayControlRPC implements the gatewaycontrol.GatewayControlRPC interface.
type GatewayControlRPC struct {
	daemon *Daemon
}

// ApplySnapshot applies a new runtime snapshot.
func (r *GatewayControlRPC) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest, resp *gatewaycontrol.ApplySnapshotResponse) error {
	log.Printf("[gatewayd] RPC: ApplySnapshot snapshot_id=%s", req.SnapshotID)

	// Validate request
	if req.SnapshotID == "" {
		resp.Applied = false
		resp.Error = "snapshot_id is required"
		log.Printf("[gatewayd] RPC ApplySnapshot error: %s", resp.Error)
		return nil
	}

	if len(req.SnapshotBytes) == 0 {
		resp.Applied = false
		resp.Error = "snapshot_bytes is required"
		log.Printf("[gatewayd] RPC ApplySnapshot error: %s", resp.Error)
		return nil
	}

	// Parse snapshot
	var snap snapshot.Snapshot
	if err := parseSnapshot(req.SnapshotBytes, &snap); err != nil {
		resp.Applied = false
		resp.Error = fmt.Sprintf("parse snapshot: %v", err)
		log.Printf("[gatewayd] RPC ApplySnapshot error: %s", resp.Error)
		return nil
	}

	// Override metadata from request
	snap.Meta.SnapshotID = req.SnapshotID
	snap.Meta.RevisionID = req.RevisionID
	snap.Meta.SchemaVersion = req.SchemaVersion
	snap.Meta.GeneratedAt = req.GeneratedAt

	// Get previous snapshot ID
	r.daemon.snapshotMu.RLock()
	var prevID string
	if r.daemon.snapshot != nil {
		prevID = r.daemon.snapshot.Meta.SnapshotID
	}
	r.daemon.snapshotMu.RUnlock()

	// Apply snapshot
	if err := r.daemon.ApplySnapshot(&snap); err != nil {
		resp.Applied = false
		resp.Error = err.Error()
		log.Printf("[gatewayd] RPC ApplySnapshot error: %s", resp.Error)
		return nil
	}

	resp.Applied = true
	resp.ActiveSnapshotID = req.SnapshotID
	resp.PreviousSnapshotID = prevID
	log.Printf("[gatewayd] RPC ApplySnapshot success: %s", req.SnapshotID)
	return nil
}

// GetStatus returns the current gatewayd status.
func (r *GatewayControlRPC) GetStatus(req gatewaycontrol.GetStatusRequest, resp *gatewaycontrol.GetStatusResponse) error {
	status := r.daemon.GetStatus()
	*resp = status
	return nil
}

// Drain signals gatewayd to drain connections.
func (r *GatewayControlRPC) Drain(req gatewaycontrol.DrainRequest, resp *gatewaycontrol.DrainResponse) error {
	log.Printf("[gatewayd] RPC: Drain timeout=%v force=%v", req.Timeout, req.Force)

	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	deadline := time.Now().Add(timeout)

	// Wait for active requests to drain
	for {
		active := r.daemon.activeReqs.Load()

		if active == 0 {
			resp.Success = true
			resp.RemainingRequests = 0
			resp.DrainedAt = timeNow()
			log.Printf("[gatewayd] drain complete: all requests finished")
			return nil
		}

		if time.Now().After(deadline) {
			resp.RemainingRequests = int(active)
			if req.Force {
				log.Printf("[gatewayd] drain timeout, force terminating with %d requests remaining", active)
				resp.Success = true
				resp.DrainedAt = timeNow()
				return nil
			}
			log.Printf("[gatewayd] drain timeout: %d requests still active", active)
			resp.Success = false
			resp.DrainedAt = timeNow()
			resp.Error = fmt.Sprintf("timeout: %d requests still active", active)
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// parseSnapshot parses snapshot bytes into a Snapshot struct.
func parseSnapshot(data []byte, snap *snapshot.Snapshot) error {
	// Try JSON first
	if err := json.Unmarshal(data, snap); err == nil {
		return nil
	}
	// Try YAML
	return yaml.Unmarshal(data, snap)
}

// yamlUnmarshal is an alias for yaml.Unmarshal.
var yamlUnmarshal = yaml.Unmarshal

// timeNow returns the current time.
var timeNow = func() time.Time { return time.Now() }

// connAdapter adapts contracts.Conn to io.ReadWriteCloser.
type connAdapter struct {
	conn contracts.Conn
}

func (c *connAdapter) Read(b []byte) (n int, err error)  { return c.conn.Read(b) }
func (c *connAdapter) Write(b []byte) (n int, err error) { return c.conn.Write(b) }
func (c *connAdapter) Close() error                      { return c.conn.Close() }

// Ensure connAdapter implements io.ReadWriteCloser
var _ io.ReadWriteCloser = (*connAdapter)(nil)
