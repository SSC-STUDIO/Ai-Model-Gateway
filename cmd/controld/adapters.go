package main

import (
	"fmt"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/publish"
)

type publisherGatewayAdapter struct {
	daemon *Daemon
}

func (a publisherGatewayAdapter) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.ApplySnapshot(req)
}

func (a publisherGatewayAdapter) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.GetStatus()
}

type benchmarkGatewayAdapter struct {
	daemon *Daemon
}

func (a benchmarkGatewayAdapter) RunBenchmarkCase(req gatewaycontrol.RunBenchmarkCaseRequest) (*gatewaycontrol.RunBenchmarkCaseResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.RunBenchmarkCase(req)
}

type configCommandsAdapter struct {
	publisher *publish.Publisher
	reloadFn  func() (*publish.PublishResult, error)
}

func (a configCommandsAdapter) Publish(revisionID string) (*publish.PublishResult, error) {
	return a.publisher.Publish(revisionID)
}

func (a configCommandsAdapter) Rollback(revisionID string) (*publish.PublishResult, error) {
	return a.publisher.Rollback(revisionID)
}

func (a configCommandsAdapter) ValidateConfig(cfg interface{}) (*publish.ConfigValidationResult, error) {
	return a.publisher.ValidateConfig(cfg)
}

func (a configCommandsAdapter) UpdateConfig(cfg interface{}, description string) (*publish.PublishResult, error) {
	return a.publisher.UpdateConfig(cfg, description)
}

func (a configCommandsAdapter) ReloadConfig() (*publish.PublishResult, error) {
	if a.reloadFn == nil {
		return nil, fmt.Errorf("reload is not configured")
	}
	return a.reloadFn()
}
