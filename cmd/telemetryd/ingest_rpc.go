// Package main contains the RPC servers for telemetryd.
package main

import (
	"log"
	"net/rpc"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/telemetryingest"
)

// IngestRPCServer handles RPC calls from gatewayd for event ingestion.
type IngestRPCServer struct {
	daemon *Daemon
	server *rpc.Server
}

// NewIngestRPCServer creates a new ingest RPC server.
func NewIngestRPCServer(daemon *Daemon) *IngestRPCServer {
	s := &IngestRPCServer{
		daemon: daemon,
		server: rpc.NewServer(),
	}
	s.server.Register(&TelemetryIngestRPC{daemon: daemon})
	return s
}

// ServeConn serves a single RPC connection.
func (s *IngestRPCServer) ServeConn(conn contracts.Conn) {
	s.server.ServeConn(&connAdapter{conn: conn})
}

// TelemetryIngestRPC implements the telemetryingest.TelemetryIngestRPC interface.
type TelemetryIngestRPC struct {
	daemon *Daemon
}

// AppendBatch appends a batch of events to the event log.
func (r *TelemetryIngestRPC) AppendBatch(req telemetryingest.AppendBatchRequest, resp *telemetryingest.AppendBatchResponse) error {
	log.Printf("[telemetryd] RPC: AppendBatch batch_id=%s count=%d", req.BatchID, len(req.Events))

	if r.daemon.eventLog == nil {
		resp.Accepted = 0
		resp.Dropped = len(req.Events)
		resp.Error = "event log not initialized"
		log.Printf("[telemetryd] RPC AppendBatch error: %s", resp.Error)
		return nil
	}

	if len(req.Events) == 0 {
		resp.Accepted = 0
		resp.Dropped = 0
		resp.HighWatermark = generateHighWatermark()
		return nil
	}

	accepted, dropped, err := r.daemon.AppendEvents(req.Events)
	if err != nil {
		resp.Accepted = 0
		resp.Dropped = len(req.Events)
		resp.Error = err.Error()
		log.Printf("[telemetryd] RPC AppendBatch error: %s", err)
		return nil
	}

	resp.Accepted = accepted
	resp.Dropped = dropped
	resp.HighWatermark = generateHighWatermark()
	log.Printf("[telemetryd] RPC AppendBatch success: %d accepted, %d dropped", accepted, dropped)
	return nil
}

// Flush flushes all buffered events.
func (r *TelemetryIngestRPC) Flush(req telemetryingest.FlushRequest, resp *telemetryingest.FlushResponse) error {
	log.Printf("[telemetryd] RPC: Flush")

	resp.Success = false
	resp.FlushedCount = 0
	resp.Error = "flush is not implemented"
	return nil
}

// Ping checks if telemetryd is healthy.
func (r *TelemetryIngestRPC) Ping(req telemetryingest.PingRequest, resp *telemetryingest.PingResponse) error {
	resp.Version = Version
	resp.ServerTime = time.Now()
	resp.EventCount = r.daemon.GetEventCount()
	resp.Healthy = true
	return nil
}

// connAdapter adapts contracts.Conn to io.ReadWriteCloser.
type connAdapter struct {
	conn contracts.Conn
}

func (c *connAdapter) Read(b []byte) (n int, err error)  { return c.conn.Read(b) }
func (c *connAdapter) Write(b []byte) (n int, err error) { return c.conn.Write(b) }
func (c *connAdapter) Close() error                      { return c.conn.Close() }

func generateHighWatermark() string {
	return "hw_" + time.Now().UTC().Format("20060102_150405.000000")
}
