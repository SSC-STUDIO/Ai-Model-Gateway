// Package main contains RPC clients for controld.
package main

import (
	"net/rpc"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
)

// GatewayClient is an RPC client for communicating with gatewayd.
type GatewayClient struct {
	conn   contracts.Conn
	client *rpc.Client
}

// NewGatewayClient creates a new gateway RPC client.
func NewGatewayClient(conn contracts.Conn) *GatewayClient {
	return &GatewayClient{
		conn:   conn,
		client: rpc.NewClient(&connAdapter{conn: conn}),
	}
}

// ApplySnapshot sends a snapshot to gatewayd.
func (c *GatewayClient) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error) {
	var resp gatewaycontrol.ApplySnapshotResponse
	err := c.client.Call("GatewayControlRPC.ApplySnapshot", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetStatus returns the current gatewayd status.
func (c *GatewayClient) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	var resp gatewaycontrol.GetStatusResponse
	err := c.client.Call("GatewayControlRPC.GetStatus", gatewaycontrol.GetStatusRequest{}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Drain signals gatewayd to drain connections.
func (c *GatewayClient) Drain(req gatewaycontrol.DrainRequest) (*gatewaycontrol.DrainResponse, error) {
	var resp gatewaycontrol.DrainResponse
	err := c.client.Call("GatewayControlRPC.Drain", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Close closes the client.
func (c *GatewayClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// connAdapter adapts contracts.Conn to io.ReadWriteCloser.
type connAdapter struct {
	conn contracts.Conn
}

func (c *connAdapter) Read(b []byte) (n int, err error)  { return c.conn.Read(b) }
func (c *connAdapter) Write(b []byte) (n int, err error) { return c.conn.Write(b) }
func (c *connAdapter) Close() error                      { return c.conn.Close() }
