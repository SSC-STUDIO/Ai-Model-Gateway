package main

import (
	"context"
	"fmt"
	"strings"

	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	"gopkg.in/yaml.v3"
)

type configToolsAdapter struct {
	publisher *publish.Publisher
	compiler  *compiler.Compiler
}

func (a configToolsAdapter) PreviewConfig(ctx context.Context, req api.ConfigPreviewRequest) (*api.ConfigPreviewResponse, error) {
	_ = ctx
	if a.compiler == nil {
		return nil, fmt.Errorf("compiler not configured")
	}
	cfg, revisionID, err := a.loadConfigForTools(req.Config, req.RevisionID)
	if err != nil {
		return nil, err
	}
	snap, err := a.compiler.CompileFromConfig(cfg)
	if err != nil {
		return &api.ConfigPreviewResponse{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}
	resp := api.SummarizeSnapshot(snap, revisionID, nil)
	return &resp, nil
}

func (a configToolsAdapter) DiffConfig(ctx context.Context, req api.ConfigDiffRequest) (*api.ConfigDiffResponse, error) {
	_ = ctx
	if a.publisher == nil {
		return nil, fmt.Errorf("publisher not configured")
	}
	before, fromRevisionID, err := a.loadBaseConfig(req.FromRevisionID)
	if err != nil {
		return nil, err
	}
	var (
		after        *core.Config
		toRevisionID string
	)
	switch {
	case req.Config != nil:
		after, err = decodeToolConfig(req.Config)
		toRevisionID = "draft"
	case strings.TrimSpace(req.ToRevisionID) != "":
		after, err = a.publisher.LoadRevisionConfig(req.ToRevisionID)
		toRevisionID = strings.TrimSpace(req.ToRevisionID)
	default:
		return nil, fmt.Errorf("config or to_revision_id is required")
	}
	if err != nil {
		return nil, err
	}
	if after == nil {
		return nil, fmt.Errorf("target config not found")
	}
	return &api.ConfigDiffResponse{
		FromRevisionID: fromRevisionID,
		ToRevisionID:   toRevisionID,
		Changes:        api.DiffConfigs(before, after),
	}, nil
}

func (a configToolsAdapter) loadConfigForTools(raw any, revisionID string) (*core.Config, string, error) {
	if raw != nil {
		cfg, err := decodeToolConfig(raw)
		return cfg, "draft", err
	}
	revisionID = strings.TrimSpace(revisionID)
	if revisionID != "" {
		cfg, err := a.publisher.LoadRevisionConfig(revisionID)
		if err != nil {
			return nil, "", err
		}
		if cfg == nil {
			return nil, "", fmt.Errorf("revision not found: %s", revisionID)
		}
		return cfg, revisionID, nil
	}
	cfg, err := a.publisher.GetCurrentConfig()
	if err != nil {
		return nil, "", err
	}
	if cfg == nil {
		return nil, "", fmt.Errorf("active config not found")
	}
	current, _ := a.publisher.GetCurrentRevision()
	if current != nil {
		revisionID = current.RevisionID
	}
	return cfg, revisionID, nil
}

func (a configToolsAdapter) loadBaseConfig(revisionID string) (*core.Config, string, error) {
	revisionID = strings.TrimSpace(revisionID)
	if revisionID != "" {
		cfg, err := a.publisher.LoadRevisionConfig(revisionID)
		if err != nil {
			return nil, "", err
		}
		if cfg == nil {
			return nil, "", fmt.Errorf("revision not found: %s", revisionID)
		}
		return cfg, revisionID, nil
	}
	cfg, err := a.publisher.GetCurrentConfig()
	if err != nil {
		return nil, "", err
	}
	if cfg == nil {
		return nil, "", fmt.Errorf("active config not found")
	}
	current, _ := a.publisher.GetCurrentRevision()
	if current != nil {
		revisionID = current.RevisionID
	}
	return cfg, revisionID, nil
}

func decodeToolConfig(raw any) (*core.Config, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	var cfg core.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.Normalize()
	return &cfg, nil
}
