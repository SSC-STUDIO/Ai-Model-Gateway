package publish

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestReplaceRevisionsStoresClonedConfigAndReportsHistory(t *testing.T) {
	publisher := NewPublisher(nil, nil)
	createdAt := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)

	firstConfig := testConfig("127.0.0.1:18080")
	secondConfig := testConfig("127.0.0.1:19090")

	err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_1",
			CreatedAt:   createdAt,
			CreatedBy:   "alice",
			Description: "first",
			Config:      firstConfig,
		},
		{
			RevisionID:  "rev_2",
			CreatedAt:   createdAt.Add(time.Minute),
			CreatedBy:   "bob",
			Description: "second",
			Config:      secondConfig,
		},
	}, "rev_2")
	if err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	secondConfig.Server.Listen = "127.0.0.1:19999"

	current, err := publisher.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != "rev_2" || !current.IsActive {
		t.Fatalf("GetCurrentRevision() = %#v, want active rev_2", current)
	}

	history, err := publisher.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(GetHistory()) = %d, want 2", len(history))
	}
	if history[0].RevisionID != "rev_2" || !history[0].IsActive {
		t.Fatalf("history[0] = %#v, want active rev_2", history[0])
	}
	if history[1].RevisionID != "rev_1" || history[1].IsActive {
		t.Fatalf("history[1] = %#v, want inactive rev_1", history[1])
	}
	if got := publisher.revisions[1].Config.Server.Listen; got != "127.0.0.1:19090" {
		t.Fatalf("stored config listen = %q, want %q", got, "127.0.0.1:19090")
	}
}

func TestReplaceRevisionsPreservesActiveRevisionWhenUnspecified(t *testing.T) {
	publisher := NewPublisher(nil, nil)
	createdAt := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)

	initial := []Revision{
		{RevisionID: "rev_1", CreatedAt: createdAt, Config: testConfig("127.0.0.1:18080")},
		{RevisionID: "rev_2", CreatedAt: createdAt.Add(time.Minute), Config: testConfig("127.0.0.1:19090")},
	}
	if err := publisher.ReplaceRevisions(initial, "rev_2"); err != nil {
		t.Fatalf("ReplaceRevisions(initial) error = %v", err)
	}

	updated := []Revision{
		{RevisionID: "rev_1", CreatedAt: createdAt, Description: "older", Config: testConfig("127.0.0.1:18080")},
		{RevisionID: "rev_2", CreatedAt: createdAt.Add(time.Minute), Description: "newer", Config: testConfig("127.0.0.1:19090")},
	}
	if err := publisher.ReplaceRevisions(updated, ""); err != nil {
		t.Fatalf("ReplaceRevisions(updated) error = %v", err)
	}

	current, err := publisher.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != "rev_2" {
		t.Fatalf("GetCurrentRevision() = %#v, want rev_2", current)
	}
	if current.Description != "newer" {
		t.Fatalf("GetCurrentRevision().Description = %q, want %q", current.Description, "newer")
	}
}

func TestUpsertRevisionPreservesExistingHistory(t *testing.T) {
	publisher := NewPublisher(nil, nil)
	createdAt := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)

	if err := publisher.ReplaceRevisions([]Revision{
		{RevisionID: "rev_1", CreatedAt: createdAt, Description: "older", Config: testConfig("127.0.0.1:18080")},
		{RevisionID: "rev_2", CreatedAt: createdAt.Add(time.Minute), Description: "current", Config: testConfig("127.0.0.1:19090")},
	}, "rev_2"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	if err := publisher.UpsertRevision(Revision{
		RevisionID:  "rev_3",
		CreatedAt:   createdAt.Add(2 * time.Minute),
		CreatedBy:   "watcher",
		Description: "reloaded from file",
		Config:      testConfig("127.0.0.1:29090"),
	}, true); err != nil {
		t.Fatalf("UpsertRevision() error = %v", err)
	}

	history, err := publisher.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("len(GetHistory()) = %d, want 3", len(history))
	}
	if history[0].RevisionID != "rev_3" || !history[0].IsActive {
		t.Fatalf("history[0] = %#v, want active rev_3", history[0])
	}
	if history[1].RevisionID != "rev_2" || history[1].IsActive {
		t.Fatalf("history[1] = %#v, want inactive rev_2", history[1])
	}
	if history[2].RevisionID != "rev_1" {
		t.Fatalf("history[2] = %#v, want rev_1", history[2])
	}
}

func TestGetCurrentConfigViewIncludesRevisionAndPolicy(t *testing.T) {
	publisher := NewPublisher(nil, nil)
	cfg := testConfig("127.0.0.1:18080")
	cfg.Normalize()
	cfg.Admin.PublishHistoryLimit = 42

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_view",
			CreatedAt:   time.Date(2026, time.April, 17, 11, 30, 0, 0, time.UTC),
			CreatedBy:   "operator",
			Description: "view test",
			Config:      cfg,
		},
	}, "rev_view"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	view, err := publisher.GetCurrentConfigView()
	if err != nil {
		t.Fatalf("GetCurrentConfigView() error = %v", err)
	}
	if view == nil || view.Revision == nil {
		t.Fatalf("GetCurrentConfigView() = %#v, want non-nil revision", view)
	}
	if view.Revision.RevisionID != "rev_view" || !view.Revision.IsActive {
		t.Fatalf("GetCurrentConfigView().Revision = %#v, want active rev_view", view.Revision)
	}
	if view.Policy.PublishHistoryLimit != 42 {
		t.Fatalf("GetCurrentConfigView().Policy.PublishHistoryLimit = %d, want 42", view.Policy.PublishHistoryLimit)
	}
}

func TestValidateConfigAcceptsMapInput(t *testing.T) {
	publisher := NewPublisher(nil, compiler.NewCompiler())

	result, err := publisher.ValidateConfig(map[string]interface{}{
		"server": map[string]interface{}{
			"listen": "127.0.0.1:18080",
		},
		"providers": []interface{}{
			map[string]interface{}{
				"name":     "test-provider",
				"base_url": "https://example.invalid/v1",
				"api_key":  "secret",
				"models":   []interface{}{"gpt-test"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateConfig(map) error = %v", err)
	}
	if result == nil || !result.Valid {
		t.Fatalf("ValidateConfig(map) = %#v, want valid", result)
	}
}

func TestUpdateConfigAcceptsMapInput(t *testing.T) {
	gateway := &stubGateway{}
	publisher := NewPublisher(gateway, compiler.NewCompiler())

	result, err := publisher.UpdateConfig(map[string]interface{}{
		"server": map[string]interface{}{
			"listen": "127.0.0.1:18080",
		},
		"providers": []interface{}{
			map[string]interface{}{
				"name":     "test-provider",
				"base_url": "https://example.invalid/v1",
				"api_key":  "secret",
				"models":   []interface{}{"gpt-test"},
			},
		},
	}, "editor save")
	if err != nil {
		t.Fatalf("UpdateConfig(map) error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("UpdateConfig(map) = %#v, want success", result)
	}
	if len(gateway.applyRequests) != 1 {
		t.Fatalf("len(applyRequests) = %d, want 1", len(gateway.applyRequests))
	}

	current, err := publisher.GetCurrentConfig()
	if err != nil {
		t.Fatalf("GetCurrentConfig() error = %v", err)
	}
	if current == nil || current.Server.Listen != "127.0.0.1:18080" {
		t.Fatalf("GetCurrentConfig() = %#v, want listen 127.0.0.1:18080", current)
	}
}

func TestPublishUsesSeededRevisionConfigAndRecordsResults(t *testing.T) {
	gateway := &stubGateway{}
	publisher := NewPublisher(gateway, nil)
	config := testConfig("127.0.0.1:19090")

	var compiled Revision
	publisher.SetRevisionCompiler(func(revision Revision) (*snapshot.Snapshot, error) {
		compiled = revision
		if revision.Config == nil {
			t.Fatal("revision compiler received nil config")
		}
		return testSnapshot(revision.RevisionID, revision.Config.Server.Listen), nil
	})

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_cfg",
			CreatedAt:   time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC),
			CreatedBy:   "operator",
			Description: "config-backed revision",
			Config:      config,
		},
	}, ""); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	config.Server.Listen = "127.0.0.1:19999"

	result, err := publisher.Publish("rev_cfg")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish() result = %#v, want success", result)
	}
	if compiled.Config == nil || compiled.Config.Server.Listen != "127.0.0.1:19090" {
		t.Fatalf("compiled revision config listen = %#v, want %q", compiled.Config, "127.0.0.1:19090")
	}

	if len(gateway.applyRequests) != 1 {
		t.Fatalf("len(applyRequests) = %d, want 1", len(gateway.applyRequests))
	}
	req := gateway.applyRequests[0]
	if req.RevisionID != "rev_cfg" {
		t.Fatalf("ApplySnapshotRequest.RevisionID = %q, want %q", req.RevisionID, "rev_cfg")
	}
	if req.SnapshotID != result.SnapshotID {
		t.Fatalf("ApplySnapshotRequest.SnapshotID = %q, want %q", req.SnapshotID, result.SnapshotID)
	}

	var published snapshot.Snapshot
	if err := json.Unmarshal(req.SnapshotBytes, &published); err != nil {
		t.Fatalf("json.Unmarshal(snapshot bytes) error = %v", err)
	}
	if published.Meta.RevisionID != "rev_cfg" {
		t.Fatalf("snapshot.Meta.RevisionID = %q, want %q", published.Meta.RevisionID, "rev_cfg")
	}
	if published.Meta.SnapshotID != result.SnapshotID {
		t.Fatalf("snapshot.Meta.SnapshotID = %q, want %q", published.Meta.SnapshotID, result.SnapshotID)
	}
	if published.Ingress.Listen != "127.0.0.1:19090" {
		t.Fatalf("snapshot.Ingress.Listen = %q, want %q", published.Ingress.Listen, "127.0.0.1:19090")
	}
	if publisher.activeIdx != 0 {
		t.Fatalf("activeIdx = %d, want 0", publisher.activeIdx)
	}
	if len(publisher.publishes) != 1 {
		t.Fatalf("len(publishes) = %d, want 1", len(publisher.publishes))
	}
	if publisher.publishes[0].Status != "observed" {
		t.Fatalf("publish status = %q, want %q", publisher.publishes[0].Status, "observed")
	}
}

func TestPublishRecordsFailedStatusOnGatewayError(t *testing.T) {
	gateway := &stubGateway{applyErr: errors.New("gateway down")}
	publisher := NewPublisher(gateway, nil)

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID: "rev_fail",
			Snapshot:   testSnapshot("rev_fail", "127.0.0.1:18080"),
		},
	}, ""); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	result, err := publisher.Publish("rev_fail")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("Publish() result = %#v, want failed result", result)
	}
	if len(publisher.publishes) != 1 {
		t.Fatalf("len(publishes) = %d, want 1", len(publisher.publishes))
	}
	if publisher.publishes[0].Status != "failed" {
		t.Fatalf("publish status = %q, want %q", publisher.publishes[0].Status, "failed")
	}
	if publisher.publishes[0].Error != "gateway down" {
		t.Fatalf("publish error = %q, want %q", publisher.publishes[0].Error, "gateway down")
	}
}

func TestPublishUsesCompilerWithStoredRevisionConfigByDefault(t *testing.T) {
	gateway := &stubGateway{}
	comp := compiler.NewCompiler()
	publisher := NewPublisher(gateway, comp)

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_default_compile",
			CreatedAt:   time.Date(2026, time.April, 18, 9, 0, 0, 0, time.UTC),
			CreatedBy:   "operator",
			Description: "default compiler path",
			Config:      testConfig("127.0.0.1:18888"),
		},
	}, ""); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	result, err := publisher.Publish("rev_default_compile")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish() result = %#v, want success", result)
	}
	if len(gateway.applyRequests) != 1 {
		t.Fatalf("len(applyRequests) = %d, want 1", len(gateway.applyRequests))
	}

	var published snapshot.Snapshot
	if err := json.Unmarshal(gateway.applyRequests[0].SnapshotBytes, &published); err != nil {
		t.Fatalf("json.Unmarshal(snapshot bytes) error = %v", err)
	}
	if published.Meta.RevisionID != "rev_default_compile" {
		t.Fatalf("snapshot.Meta.RevisionID = %q, want %q", published.Meta.RevisionID, "rev_default_compile")
	}
	if published.Ingress.Listen != "127.0.0.1:18888" {
		t.Fatalf("snapshot.Ingress.Listen = %q, want %q", published.Ingress.Listen, "127.0.0.1:18888")
	}
}

func TestPublishRejectsRevisionWithoutConfigOrSnapshot(t *testing.T) {
	publisher := NewPublisher(&stubGateway{}, compiler.NewCompiler())

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID: "rev_missing_config",
		},
	}, ""); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	result, err := publisher.Publish("rev_missing_config")
	if err == nil {
		t.Fatal("expected publish error for revision without config or snapshot")
	}
	if result != nil {
		t.Fatalf("expected nil result on hard compile error, got %#v", result)
	}
	if !strings.Contains(err.Error(), "has no config payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublisherPersistsAndRestoresState(t *testing.T) {
	stateStore := NewFileStateStore(filepath.Join(t.TempDir(), "publisher-state.json"))
	gateway := &stubGateway{}

	publisher := NewPublisher(gateway, nil)
	publisher.SetStateStore(stateStore)
	publisher.SetRevisionCompiler(func(revision Revision) (*snapshot.Snapshot, error) {
		return testSnapshot(revision.RevisionID, revision.Config.Server.Listen), nil
	})

	revisions := []Revision{
		{
			RevisionID:  "rev_1",
			CreatedAt:   time.Date(2026, time.April, 17, 13, 0, 0, 0, time.UTC),
			CreatedBy:   "alice",
			Description: "first",
			Config:      testConfig("127.0.0.1:18080"),
		},
		{
			RevisionID:  "rev_2",
			CreatedAt:   time.Date(2026, time.April, 17, 14, 0, 0, 0, time.UTC),
			CreatedBy:   "bob",
			Description: "second",
			Config:      testConfig("127.0.0.1:19090"),
		},
	}
	if err := publisher.ReplaceRevisions(revisions, "rev_2"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}
	result, err := publisher.Publish("rev_2")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish() result = %#v, want success", result)
	}

	restored := NewPublisher(&stubGateway{}, nil)
	restored.SetStateStore(stateStore)
	loaded, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if !loaded {
		t.Fatal("expected persisted state to load")
	}

	current, err := restored.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != "rev_2" || !current.IsActive {
		t.Fatalf("GetCurrentRevision() = %#v, want active rev_2", current)
	}

	cfg, err := restored.GetCurrentConfig()
	if err != nil {
		t.Fatalf("GetCurrentConfig() error = %v", err)
	}
	if cfg == nil || cfg.Server.Listen != "127.0.0.1:19090" {
		t.Fatalf("GetCurrentConfig() = %#v, want listen 127.0.0.1:19090", cfg)
	}

	history, err := restored.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 2 || history[0].RevisionID != "rev_2" || history[1].RevisionID != "rev_1" {
		t.Fatalf("history = %#v, want rev_2 then rev_1", history)
	}
	if len(restored.publishes) != 1 || restored.publishes[0].Status != "observed" {
		t.Fatalf("publishes = %#v, want one observed publish", restored.publishes)
	}
}

func TestSQLiteStateStorePersistsAndRestoresState(t *testing.T) {
	stateStore := NewSQLiteStateStore(filepath.Join(t.TempDir(), "publisher-state.db"))
	gateway := &stubGateway{}

	publisher := NewPublisher(gateway, nil)
	publisher.SetStateStore(stateStore)
	publisher.SetRevisionCompiler(func(revision Revision) (*snapshot.Snapshot, error) {
		return testSnapshot(revision.RevisionID, revision.Config.Server.Listen), nil
	})

	revisions := []Revision{
		{
			RevisionID:  "rev_sqlite_1",
			CreatedAt:   time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
			CreatedBy:   "alice",
			Description: "sqlite first",
			Config:      testConfig("127.0.0.1:18080"),
		},
		{
			RevisionID:  "rev_sqlite_2",
			CreatedAt:   time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC),
			CreatedBy:   "bob",
			Description: "sqlite second",
			Config:      testConfig("127.0.0.1:19090"),
		},
	}
	if err := publisher.ReplaceRevisions(revisions, "rev_sqlite_2"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}
	result, err := publisher.Publish("rev_sqlite_2")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish() result = %#v, want success", result)
	}

	restored := NewPublisher(&stubGateway{}, nil)
	restored.SetStateStore(stateStore)
	loaded, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if !loaded {
		t.Fatal("expected persisted sqlite state to load")
	}

	current, err := restored.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != "rev_sqlite_2" || !current.IsActive {
		t.Fatalf("GetCurrentRevision() = %#v, want active rev_sqlite_2", current)
	}
	if len(restored.publishes) != 1 || restored.publishes[0].RevisionID != "rev_sqlite_2" {
		t.Fatalf("publishes = %#v, want observed publish for rev_sqlite_2", restored.publishes)
	}
}

func TestMigratingStateStoreLoadsLegacyJSONIntoSQLite(t *testing.T) {
	tempDir := t.TempDir()
	legacy := NewFileStateStore(filepath.Join(tempDir, "publisher-state.json"))
	primary := NewSQLiteStateStore(filepath.Join(tempDir, "publisher-state.db"))
	stateStore := NewMigratingStateStore(primary, legacy)

	storedRevision, err := marshalStoredRevision(Revision{
		RevisionID:  "rev_legacy",
		CreatedAt:   time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC),
		CreatedBy:   "system",
		Description: "legacy",
		Config:      testConfig("127.0.0.1:18080"),
		Snapshot:    testSnapshot("rev_legacy", "127.0.0.1:18080"),
	})
	if err != nil {
		t.Fatalf("marshalStoredRevision() error = %v", err)
	}
	legacyState := &PublisherState{
		Version:          currentStateVersion,
		ActiveRevisionID: "rev_legacy",
		Revisions:        []StoredRevision{storedRevision},
		Publishes: []PublishRecord{
			{
				PublishID:   "pub_legacy",
				RevisionID:  "rev_legacy",
				SnapshotID:  "snap_legacy",
				RequestedAt: time.Date(2026, time.April, 18, 12, 5, 0, 0, time.UTC),
				RequestedBy: "system",
				Kind:        "publish",
				Status:      "observed",
				ObservedAt:  time.Date(2026, time.April, 18, 12, 5, 5, 0, time.UTC),
			},
		},
	}
	if err := legacy.Save(legacyState); err != nil {
		t.Fatalf("legacy.Save() error = %v", err)
	}

	loadedState, err := stateStore.Load()
	if err != nil {
		t.Fatalf("stateStore.Load() error = %v", err)
	}
	if loadedState == nil || loadedState.ActiveRevisionID != "rev_legacy" {
		t.Fatalf("stateStore.Load() = %#v, want active rev_legacy", loadedState)
	}

	primaryLoaded, err := primary.Load()
	if err != nil {
		t.Fatalf("primary.Load() error = %v", err)
	}
	if primaryLoaded == nil || primaryLoaded.ActiveRevisionID != "rev_legacy" {
		t.Fatalf("primary.Load() = %#v, want migrated active rev_legacy", primaryLoaded)
	}
	if len(primaryLoaded.Publishes) != 1 || primaryLoaded.Publishes[0].PublishID != "pub_legacy" {
		t.Fatalf("primaryLoaded.Publishes = %#v, want migrated publish", primaryLoaded.Publishes)
	}
}

func TestPublisherTrimsPublishLedgerToRetentionLimit(t *testing.T) {
	stateStore := NewSQLiteStateStore(filepath.Join(t.TempDir(), "publisher-state.db"))
	gateway := &stubGateway{}

	publisher := NewPublisher(gateway, nil)
	publisher.SetStateStore(stateStore)
	publisher.SetRevisionCompiler(func(revision Revision) (*snapshot.Snapshot, error) {
		return testSnapshot(revision.RevisionID, revision.Config.Server.Listen), nil
	})

	cfg := testConfig("127.0.0.1:18080")
	cfg.Normalize()
	cfg.Admin.PublishHistoryLimit = 2

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_trim",
			CreatedAt:   time.Date(2026, time.April, 18, 13, 0, 0, 0, time.UTC),
			CreatedBy:   "operator",
			Description: "trim me",
			Config:      cfg,
		},
	}, "rev_trim"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	var publishIDs []string
	for i := 0; i < 4; i++ {
		result, err := publisher.Publish("rev_trim")
		if err != nil {
			t.Fatalf("Publish() #%d error = %v", i+1, err)
		}
		if result == nil || !result.Success {
			t.Fatalf("Publish() #%d result = %#v, want success", i+1, result)
		}
		publishIDs = append(publishIDs, publisher.publishes[len(publisher.publishes)-1].PublishID)
	}

	if len(publisher.publishes) != 2 {
		t.Fatalf("len(publishes) = %d, want 2", len(publisher.publishes))
	}
	if publisher.publishes[0].PublishID != publishIDs[2] || publisher.publishes[1].PublishID != publishIDs[3] {
		t.Fatalf("publishes = %#v, want last two publish IDs %q and %q", publisher.publishes, publishIDs[2], publishIDs[3])
	}

	restored := NewPublisher(&stubGateway{}, nil)
	restored.SetStateStore(stateStore)
	loaded, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if !loaded {
		t.Fatal("expected trimmed state to load")
	}
	if len(restored.publishes) != 2 {
		t.Fatalf("len(restored.publishes) = %d, want 2", len(restored.publishes))
	}
	if restored.publishes[0].PublishID != publishIDs[2] || restored.publishes[1].PublishID != publishIDs[3] {
		t.Fatalf("restored.publishes = %#v, want last two publish IDs %q and %q", restored.publishes, publishIDs[2], publishIDs[3])
	}
	policy, err := restored.GetPolicy()
	if err != nil {
		t.Fatalf("GetPolicy() error = %v", err)
	}
	if policy.PublishHistoryLimit != 2 {
		t.Fatalf("GetPolicy().PublishHistoryLimit = %d, want 2", policy.PublishHistoryLimit)
	}
}

func TestPublisherUsesActiveRevisionConfiguredPublishHistoryLimit(t *testing.T) {
	publisher := NewPublisher(&stubGateway{}, nil)
	publisher.SetRevisionCompiler(func(revision Revision) (*snapshot.Snapshot, error) {
		return testSnapshot(revision.RevisionID, revision.Config.Server.Listen), nil
	})

	cfg := testConfig("127.0.0.1:18080")
	cfg.Normalize()
	cfg.Admin.PublishHistoryLimit = 3

	if err := publisher.ReplaceRevisions([]Revision{
		{
			RevisionID:  "rev_cfg_limit",
			CreatedAt:   time.Date(2026, time.April, 18, 14, 0, 0, 0, time.UTC),
			CreatedBy:   "operator",
			Description: "config-driven retention",
			Config:      cfg,
		},
	}, "rev_cfg_limit"); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	policy, err := publisher.GetPolicy()
	if err != nil {
		t.Fatalf("GetPolicy() error = %v", err)
	}
	if policy.PublishHistoryLimit != 3 {
		t.Fatalf("GetPolicy().PublishHistoryLimit = %d, want 3", policy.PublishHistoryLimit)
	}

	for i := 0; i < 5; i++ {
		result, err := publisher.Publish("rev_cfg_limit")
		if err != nil {
			t.Fatalf("Publish() #%d error = %v", i+1, err)
		}
		if result == nil || !result.Success {
			t.Fatalf("Publish() #%d result = %#v, want success", i+1, result)
		}
	}

	if len(publisher.publishes) != 3 {
		t.Fatalf("len(publishes) = %d, want 3", len(publisher.publishes))
	}
}

type stubGateway struct {
	applyRequests []gatewaycontrol.ApplySnapshotRequest
	applyResp     *gatewaycontrol.ApplySnapshotResponse
	applyErr      error
}

func (g *stubGateway) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error) {
	g.applyRequests = append(g.applyRequests, req)
	if g.applyErr != nil {
		return nil, g.applyErr
	}
	if g.applyResp != nil {
		resp := *g.applyResp
		return &resp, nil
	}
	return &gatewaycontrol.ApplySnapshotResponse{
		Applied:          true,
		ActiveSnapshotID: req.SnapshotID,
	}, nil
}

func (g *stubGateway) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	return &gatewaycontrol.GetStatusResponse{}, nil
}

func testConfig(listen string) *core.Config {
	return &core.Config{
		Server: core.ServerConfig{
			Listen: listen,
		},
		Providers: []core.Provider{
			{
				Name:    "demo",
				BaseURL: "https://example.com/v1",
				Models:  []string{"gpt-4o-mini"},
			},
		},
	}
}

func testSnapshot(revisionID, listen string) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			RevisionID:      revisionID,
			SchemaVersion:   snapshot.CurrentSchemaVersion,
			CompilerVersion: "test",
		},
		Ingress: snapshot.IngressConfig{
			Listen: listen,
		},
		Contract: snapshot.ContractConfig{
			PublicAPI:     "openai_chat_completions",
			EnabledRoutes: []string{"POST /v1/chat/completions"},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID:      "demo",
				ProtocolAdapter: "openai",
				BaseURL:         "https://example.com/v1",
				ModelTable: []snapshot.ModelMapping{
					{
						PublicModel:   "gpt-4o-mini",
						UpstreamModel: "gpt-4o-mini",
					},
				},
				CapabilityTable: snapshot.CapabilityTable{
					SupportsChatCompletions: true,
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:       true,
					Weight:        1,
					TimeoutMs:     30000,
					ProviderClass: "quota_limited",
				},
			},
		},
		RoutingPolicy: snapshot.RoutingPolicy{
			MaxRetries: 1,
			RetryBackoff: snapshot.RetryBackoff{
				InitialMs: 100,
				MaxMs:     500,
			},
			Health: snapshot.HealthConfig{
				Enabled:     true,
				IntervalSec: 10,
				TimeoutMs:   1000,
				Path:        "/v1/models",
			},
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:                5,
				CooldownSec:              30,
				QuotaRecoveryIntervalMin: 10,
			},
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{429},
			},
		},
		TelemetryEmit: snapshot.TelemetryEmitConfig{
			Channel: "telemetry-ingest",
			Batching: snapshot.BatchingConfig{
				MaxBatchSize:    1,
				FlushIntervalMs: 100,
			},
		},
	}
}
