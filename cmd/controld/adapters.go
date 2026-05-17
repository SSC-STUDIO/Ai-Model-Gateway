package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/publish"

	"gopkg.in/yaml.v3"
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
	publisher  *publish.Publisher
	reloadFn   func() (*publish.PublishResult, error)
	configPath string
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
	result, err := a.publisher.UpdateConfig(cfg, description)
	if err != nil {
		return result, err
	}
	if result != nil && !result.Success {
		return result, nil
	}
	if err := persistAuthoringConfig(a.configPath, cfg); err != nil {
		return nil, err
	}
	return result, nil
}

func (a configCommandsAdapter) ReloadConfig() (*publish.PublishResult, error) {
	if a.reloadFn == nil {
		return nil, fmt.Errorf("reload is not configured")
	}
	return a.reloadFn()
}

func persistAuthoringConfig(configPath string, cfg interface{}) error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal authoring config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create authoring config directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(configPath), "."+filepath.Base(configPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create authoring config temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write authoring config temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close authoring config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("replace authoring config: %w", err)
	}
	cleanup = false
	return nil
}
