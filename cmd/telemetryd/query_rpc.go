// Package main contains the RPC servers for telemetryd.
package main

import (
	"fmt"
	"log"
	"net/rpc"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/telemetryquery"
)

// QueryRPCServer handles RPC calls from controld for telemetry queries.
type QueryRPCServer struct {
	daemon *Daemon
	server *rpc.Server
}

// NewQueryRPCServer creates a new query RPC server.
func NewQueryRPCServer(daemon *Daemon) *QueryRPCServer {
	s := &QueryRPCServer{
		daemon: daemon,
		server: rpc.NewServer(),
	}
	s.server.Register(&TelemetryQueryRPC{daemon: daemon})
	return s
}

// ServeConn serves a single RPC connection.
func (s *QueryRPCServer) ServeConn(conn contracts.Conn) {
	s.server.ServeConn(&connAdapter{conn: conn})
}

// TelemetryQueryRPC implements the telemetryquery.TelemetryQueryRPC interface.
type TelemetryQueryRPC struct {
	daemon *Daemon
}

// GetOverview returns dashboard overview metrics.
func (r *TelemetryQueryRPC) GetOverview(req telemetryquery.OverviewRequest, resp *telemetryquery.OverviewResponse) error {
	log.Printf("[telemetryd] RPC: GetOverview")

	if r.daemon.queryStore == nil {
		return fmt.Errorf("query store not initialized")
	}

	resp.Windows = make(map[string]telemetryquery.WindowMetrics, len(req.WindowSets))
	for _, w := range req.WindowSets {
		metrics, err := r.daemon.queryStore.QueryWindowMetrics(w.Duration)
		if err != nil {
			return err
		}
		resp.Windows[w.Name] = metrics
	}

	models, err := r.daemon.queryStore.ListAvailableModels()
	if err != nil {
		return err
	}
	resp.AvailableModels = models
	return nil
}

// GetTelemetry returns recent telemetry events.
func (r *TelemetryQueryRPC) GetTelemetry(req telemetryquery.TelemetryRequest, resp *telemetryquery.TelemetryResponse) error {
	log.Printf("[telemetryd] RPC: GetTelemetry window=%dh limit=%d", req.WindowHours, req.Limit)

	if r.daemon.queryStore == nil {
		return fmt.Errorf("query store not initialized")
	}

	events, total, windowHours, err := r.daemon.queryStore.QueryTelemetry(req)
	if err != nil {
		return err
	}
	resp.Events = events
	resp.Total = total
	resp.WindowHours = windowHours

	models, upstreams, _, err := r.daemon.queryStore.QueryTelemetryDistributions(req)
	if err != nil {
		return err
	}
	resp.Models = models
	resp.Upstreams = upstreams
	if req.Filters.SyntheticKind == "" && req.Filters.BenchmarkRunID == "" && req.Filters.BenchmarkCaseID == "" {
		resp.Pricing = r.daemon.queryStore.QueryPricingEconomics(windowHours)
	}
	return nil
}

// GetTimeSeries returns time-bucketed metrics.
func (r *TelemetryQueryRPC) GetTimeSeries(req telemetryquery.TimeSeriesRequest, resp *telemetryquery.TimeSeriesResponse) error {
	log.Printf("[telemetryd] RPC: GetTimeSeries window=%dh bucket=%dm", req.WindowHours, req.BucketMinutes)

	if r.daemon.queryStore == nil {
		return fmt.Errorf("query store not initialized")
	}

	buckets, windowHours, bucketMinutes, err := r.daemon.queryStore.QueryTimeSeries(req)
	if err != nil {
		return err
	}
	resp.Buckets = buckets
	resp.WindowHours = windowHours
	resp.BucketMinutes = bucketMinutes
	return nil
}

// GetModelBenchmark returns model benchmark metrics.
func (r *TelemetryQueryRPC) GetModelBenchmark(req telemetryquery.BenchmarkRequest, resp *telemetryquery.BenchmarkResponse) error {
	log.Printf("[telemetryd] RPC: GetModelBenchmark window=%dh group=%s models=%v", req.WindowHours, req.Group, req.Models)

	if r.daemon.queryStore == nil {
		return fmt.Errorf("query store not initialized")
	}

	benchmarks, windowHours, err := r.daemon.queryStore.QueryModelBenchmark(req)
	if err != nil {
		return err
	}
	resp.Benchmarks = benchmarks
	resp.WindowHours = windowHours
	resp.ModelCount = len(benchmarks)
	resp.Group = normalizeBenchmarkResponseGroup(req.Group)
	return nil
}

func normalizeBenchmarkResponseGroup(group string) string {
	switch strings.ToLower(strings.TrimSpace(group)) {
	case "upstream", "provider", "providers":
		return "upstream"
	default:
		return "model"
	}
}

// Ping checks if telemetryd is healthy.
func (r *TelemetryQueryRPC) Ping(req telemetryquery.PingRequest, resp *telemetryquery.PingResponse) error {
	resp.Version = Version
	resp.ServerTime = time.Now()
	resp.EventCount = r.daemon.GetEventCount()
	resp.Healthy = true
	return nil
}
