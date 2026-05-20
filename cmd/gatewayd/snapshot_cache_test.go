package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestSnapshotDiskCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	d1, err := NewDaemon(Config{Listen: "127.0.0.1:18080", DataDir: dir})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	d1.runCtx = ctx

	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-disk",
			SchemaVersion: snapshot.CurrentSchemaVersion,
			RevisionID:    "rev-disk",
			GeneratedAt:   time.Now().UTC(),
		},
		Ingress: snapshot.IngressConfig{
			Listen: "127.0.0.1:18080",
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "test-provider",
				BaseURL:    "https://api.example.com",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
				},
			},
		},
	}
	snapBytes, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := gatewaycontrol.ApplySnapshotRequest{
		SnapshotID:    "snap-disk",
		SnapshotBytes: snapBytes,
		RevisionID:    "rev-disk",
		SchemaVersion: snapshot.CurrentSchemaVersion,
		GeneratedAt:   snap.Meta.GeneratedAt,
	}
	rpc := &GatewayControlRPC{daemon: d1}
	var resp gatewaycontrol.ApplySnapshotResponse
	if err := rpc.ApplySnapshot(req, &resp); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("ApplySnapshot not applied: %s", resp.Error)
	}

	d2, err := NewDaemon(Config{Listen: "127.0.0.1:18080", DataDir: dir})
	if err != nil {
		t.Fatalf("NewDaemon2: %v", err)
	}
	d2.runCtx = ctx
	d2.tryRestoreSnapshotFromDisk()

	st := d2.GetStatus()
	if st.Readiness != gatewaycontrol.ReadinessReady {
		t.Fatalf("after restore Readiness = %v, want Ready", st.Readiness)
	}
	if st.ActiveSnapshotID != "snap-disk" {
		t.Fatalf("ActiveSnapshotID = %q", st.ActiveSnapshotID)
	}
}

func TestSnapshotDiskRestoreWithoutMetaFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-nometa",
			SchemaVersion: snapshot.CurrentSchemaVersion,
			RevisionID:    "rev-nometa",
			GeneratedAt:   time.Now().UTC(),
		},
		Ingress: snapshot.IngressConfig{Listen: "127.0.0.1:18080"},
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "p", BaseURL: "https://api.example.com", ModelTable: []snapshot.ModelMapping{{PublicModel: "m", UpstreamModel: "m"}}},
		},
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(dir, snapshotCachePayloadFile)
	if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080", DataDir: dir})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	d.runCtx = ctx
	d.tryRestoreSnapshotFromDisk()

	st := d.GetStatus()
	if st.Readiness != gatewaycontrol.ReadinessReady {
		t.Fatalf("Readiness = %v, want Ready", st.Readiness)
	}
	if st.ActiveSnapshotID != "snap-nometa" {
		t.Fatalf("ActiveSnapshotID = %q", st.ActiveSnapshotID)
	}
}
