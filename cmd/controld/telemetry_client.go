// Package main contains RPC clients for controld.
package main

import (
	"net/rpc"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/telemetryquery"
)

// TelemetryClient is an RPC client for communicating with telemetryd.
type TelemetryClient struct {
	conn   contracts.Conn
	client *rpc.Client
}

// NewTelemetryClient creates a new telemetry RPC client.
func NewTelemetryClient(conn contracts.Conn) *TelemetryClient {
	return &TelemetryClient{
		conn:   conn,
		client: rpc.NewClient(&connAdapter{conn: conn}),
	}
}

// GetOverview returns dashboard overview metrics.
func (c *TelemetryClient) GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error) {
	var resp telemetryquery.OverviewResponse
	err := c.client.Call("TelemetryQueryRPC.GetOverview", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTelemetry returns recent telemetry events.
func (c *TelemetryClient) GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error) {
	var resp telemetryquery.TelemetryResponse
	err := c.client.Call("TelemetryQueryRPC.GetTelemetry", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTimeSeries returns time-bucketed metrics.
func (c *TelemetryClient) GetTimeSeries(req telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error) {
	var resp telemetryquery.TimeSeriesResponse
	err := c.client.Call("TelemetryQueryRPC.GetTimeSeries", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetModelBenchmark returns model benchmark metrics.
func (c *TelemetryClient) GetModelBenchmark(req telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error) {
	var resp telemetryquery.BenchmarkResponse
	err := c.client.Call("TelemetryQueryRPC.GetModelBenchmark", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Ping checks if telemetryd is healthy.
func (c *TelemetryClient) Ping() (*telemetryquery.PingResponse, error) {
	var resp telemetryquery.PingResponse
	err := c.client.Call("TelemetryQueryRPC.Ping", telemetryquery.PingRequest{}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Close closes the client.
func (c *TelemetryClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}
