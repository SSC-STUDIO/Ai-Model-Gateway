// Package main contains the RPC client for gatewayd.
package main

import (
	"net/rpc"

	"ai-model-gateway/internal/contracts"
)

// RPCClient is an RPC client for communicating with other daemons.
type RPCClient struct {
	conn   contracts.Conn
	client *rpc.Client
}

// NewRPCClient creates a new RPC client from a connection.
func NewRPCClient(conn contracts.Conn) *RPCClient {
	return &RPCClient{
		conn:   conn,
		client: rpc.NewClient(&connAdapter{conn: conn}),
	}
}

// Call makes an RPC call.
func (c *RPCClient) Call(method string, args interface{}, reply interface{}) error {
	return c.client.Call(method, args, reply)
}

// Close closes the client.
func (c *RPCClient) Close() error {
	return c.client.Close()
}
