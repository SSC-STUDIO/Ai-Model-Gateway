package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/infra/logger"

	"gopkg.in/yaml.v3"
)

const (
	snapshotCachePayloadFile = "last-applied-snapshot.payload"
	snapshotCacheMetaFile    = "last-applied-snapshot.meta.json"
)

// snapshotDiskMeta is written alongside the raw snapshot payload so a cold
// gatewayd start can re-apply the same RPC metadata overrides as controld.
type snapshotDiskMeta struct {
	SnapshotID    string `json:"snapshot_id"`
	RevisionID    string `json:"revision_id"`
	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at,omitempty"`
}

// parseSnapshot parses snapshot bytes into a Snapshot struct.
func parseSnapshot(data []byte, snap *snapshot.Snapshot) error {
	if err := json.Unmarshal(data, snap); err == nil {
		return nil
	}
	return yaml.Unmarshal(data, snap)
}

func snapshotCachePaths(dataDir string) (metaPath, payloadPath string) {
	return filepath.Join(dataDir, snapshotCacheMetaFile),
		filepath.Join(dataDir, snapshotCachePayloadFile)
}

func gatewayDataDir(cfg Config) string {
	dir := strings.TrimSpace(cfg.DataDir)
	if dir == "" {
		dir = "data"
	}
	return filepath.Clean(dir)
}

// applySnapshotFromControlRequest parses payload and applies the same metadata
// merge rules as the gatewaycontrol RPC handler.
func (d *Daemon) applySnapshotFromControlRequest(req gatewaycontrol.ApplySnapshotRequest) error {
	if req.SnapshotID == "" {
		return fmt.Errorf("snapshot_id is required")
	}
	if len(req.SnapshotBytes) == 0 {
		return fmt.Errorf("snapshot_bytes is required")
	}
	var snap snapshot.Snapshot
	if err := parseSnapshot(req.SnapshotBytes, &snap); err != nil {
		return fmt.Errorf("parse snapshot: %w", err)
	}
	snap.Meta.SnapshotID = req.SnapshotID
	snap.Meta.RevisionID = req.RevisionID
	snap.Meta.SchemaVersion = req.SchemaVersion
	snap.Meta.GeneratedAt = req.GeneratedAt
	return d.ApplySnapshot(&snap)
}

// persistLastAppliedSnapshot writes the exact snapshot bytes from controld so
// a later gateway-only restart can recover without waiting for publish.
func (d *Daemon) persistLastAppliedSnapshot(req gatewaycontrol.ApplySnapshotRequest) {
	dir := gatewayDataDir(d.config)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("snapshot cache: could not create data dir", "dir", dir, "error", err)
		return
	}
	metaPath, payloadPath := snapshotCachePaths(dir)
	meta := snapshotDiskMeta{
		SnapshotID:    req.SnapshotID,
		RevisionID:    req.RevisionID,
		SchemaVersion: req.SchemaVersion,
	}
	if !req.GeneratedAt.IsZero() {
		meta.GeneratedAt = req.GeneratedAt.UTC().Format(time.RFC3339Nano)
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		logger.Warn("snapshot cache: marshal meta", "error", err)
		return
	}
	tmpPayload := payloadPath + ".tmp"
	if err := os.WriteFile(tmpPayload, req.SnapshotBytes, 0o600); err != nil {
		logger.Warn("snapshot cache: write payload", "error", err)
		return
	}
	if err := os.Rename(tmpPayload, payloadPath); err != nil {
		_ = os.Remove(tmpPayload)
		logger.Warn("snapshot cache: rename payload", "error", err)
		return
	}
	tmpMeta := metaPath + ".tmp"
	if err := os.WriteFile(tmpMeta, metaBytes, 0o600); err != nil {
		logger.Warn("snapshot cache: write meta", "error", err)
		return
	}
	if err := os.Rename(tmpMeta, metaPath); err != nil {
		_ = os.Remove(tmpMeta)
		logger.Warn("snapshot cache: rename meta", "error", err)
		return
	}
}

// tryRestoreSnapshotFromDisk loads the last snapshot from data_dir if present.
func (d *Daemon) tryRestoreSnapshotFromDisk() {
	dir := gatewayDataDir(d.config)
	metaPath, payloadPath := snapshotCachePaths(dir)
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("snapshot cache: read payload", "error", err)
		}
		return
	}
	metaBytes, metaErr := os.ReadFile(metaPath)
	if metaErr != nil && !os.IsNotExist(metaErr) {
		logger.Warn("snapshot cache: read meta", "error", metaErr)
		return
	}
	req := gatewaycontrol.ApplySnapshotRequest{SnapshotBytes: payload}
	if len(metaBytes) > 0 {
		var diskMeta snapshotDiskMeta
		if err := json.Unmarshal(metaBytes, &diskMeta); err != nil {
			logger.Warn("snapshot cache: parse meta", "error", err)
			req.SnapshotID = ""
		} else {
			req.SnapshotID = diskMeta.SnapshotID
			req.RevisionID = diskMeta.RevisionID
			req.SchemaVersion = diskMeta.SchemaVersion
			if diskMeta.GeneratedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, diskMeta.GeneratedAt); err == nil {
					req.GeneratedAt = t
				} else if t, err := time.Parse(time.RFC3339, diskMeta.GeneratedAt); err == nil {
					req.GeneratedAt = t
				}
			}
		}
	}
	if strings.TrimSpace(req.SnapshotID) == "" {
		var snap snapshot.Snapshot
		if err := parseSnapshot(payload, &snap); err != nil {
			logger.Warn("snapshot cache: parse payload for meta-less restore", "error", err)
			return
		}
		req.SnapshotID = strings.TrimSpace(snap.Meta.SnapshotID)
		if req.SchemaVersion == 0 {
			req.SchemaVersion = snap.Meta.SchemaVersion
		}
		if req.RevisionID == "" {
			req.RevisionID = snap.Meta.RevisionID
		}
		if req.GeneratedAt.IsZero() && !snap.Meta.GeneratedAt.IsZero() {
			req.GeneratedAt = snap.Meta.GeneratedAt
		}
	}
	if strings.TrimSpace(req.SnapshotID) == "" {
		req.SnapshotID = "disk-cache"
	}
	if err := d.applySnapshotFromControlRequest(req); err != nil {
		logger.Warn("snapshot cache: restore failed", "error", err)
		return
	}
	d.recordAutoRemediation("restored_snapshot_disk_cache")
	logger.Info("restored snapshot from disk cache", "snapshot_id", req.SnapshotID, "revision_id", req.RevisionID)
}
