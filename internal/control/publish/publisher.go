// Package publish handles configuration publishing to gatewayd.
package publish

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"

	"gopkg.in/yaml.v3"
)

// Publisher handles publishing configurations to gatewayd.
type Publisher struct {
	mu               sync.RWMutex
	gateway          GatewayRPC
	compiler         *compiler.Compiler
	revisionCompiler RevisionCompileFunc
	stateStore       StateStore
	policy           PublisherPolicy

	// State
	revisions []Revision
	activeIdx int
	publishes []PublishRecord
}

// GatewayRPC is the interface for gateway RPC.
type GatewayRPC interface {
	ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error)
	GetStatus() (*gatewaycontrol.GetStatusResponse, error)
}

// Revision represents a config revision.
type Revision struct {
	RevisionID  string
	CreatedAt   time.Time
	CreatedBy   string
	Description string
	Config      *core.Config
	Snapshot    *snapshot.Snapshot
}

// RevisionCompileFunc compiles a revision into a runtime snapshot.
type RevisionCompileFunc func(revision Revision) (*snapshot.Snapshot, error)

// PublishRecord represents a publish event.
type PublishRecord struct {
	PublishID   string
	RevisionID  string
	SnapshotID  string
	RequestedAt time.Time
	RequestedBy string
	Kind        string // "publish" or "rollback"
	Status      string // "staged", "observed", "failed"
	Error       string
	ObservedAt  time.Time
}

// PublisherPolicy contains runtime policy for publisher ledger behavior.
type PublisherPolicy struct {
	PublishHistoryLimit int `json:"publish_history_limit"`
}

// CurrentConfigView is the read-model payload for the active config metadata.
type CurrentConfigView struct {
	Revision *RevisionInfo   `json:"revision"`
	Policy   PublisherPolicy `json:"policy"`
	Config   *core.Config    `json:"config,omitempty"`
	RawYAML  string          `json:"raw_yaml,omitempty"`
}

// NewPublisher creates a new publisher.
func NewPublisher(gateway GatewayRPC, comp *compiler.Compiler) *Publisher {
	return &Publisher{
		gateway:   gateway,
		compiler:  comp,
		policy:    NormalizePublisherPolicy(PublisherPolicy{}),
		revisions: make([]Revision, 0),
		publishes: make([]PublishRecord, 0),
		activeIdx: -1,
	}
}

// SetStateStore configures optional durable persistence for the publisher ledger.
func (p *Publisher) SetStateStore(store StateStore) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stateStore = store
}

// SetRevisionCompiler overrides how revisions are compiled into snapshots.
func (p *Publisher) SetRevisionCompiler(fn RevisionCompileFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.revisionCompiler = fn
}

// SetPublishRetention configures how many publish ledger entries are retained.
// Values <= 0 reset to the default retention.
func (p *Publisher) SetPublishRetention(limit int) {
	p.SetPolicy(PublisherPolicy{PublishHistoryLimit: limit})
}

// SetPolicy configures explicit publisher runtime policy.
func (p *Publisher) SetPolicy(policy PublisherPolicy) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.policy = NormalizePublisherPolicy(policy)
	p.trimPublishesLocked()
}

// GetPolicy returns the active publisher runtime policy.
func (p *Publisher) GetPolicy() (PublisherPolicy, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.policy, nil
}

// GetCurrentConfigView returns the active revision metadata together with the
// active publisher runtime policy and config content.
func (p *Publisher) GetCurrentConfigView() (*CurrentConfigView, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	view := &CurrentConfigView{
		Revision: p.currentRevisionInfoLocked(),
		Policy:   p.policy,
	}

	// Include active revision's config if available
	if p.activeIdx >= 0 && p.activeIdx < len(p.revisions) {
		rev := p.revisions[p.activeIdx]
		view.Config = rev.Config
		// Also include raw YAML if available
		if rev.Config != nil {
			if yamlBytes, err := yaml.Marshal(rev.Config); err == nil {
				view.RawYAML = string(yamlBytes)
			}
		}
	}

	return view, nil
}

// LoadState restores publisher state from the configured state store.
func (p *Publisher) LoadState() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stateStore == nil {
		return false, nil
	}

	state, err := p.stateStore.Load()
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	if err := p.restoreStateLocked(*state); err != nil {
		return false, err
	}
	return true, nil
}

// ReplaceRevisions replaces the in-memory revision ledger with the supplied revisions.
// Revisions are stored in the provided order.
func (p *Publisher) ReplaceRevisions(revisions []Revision, activeRevisionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	cloned := make([]Revision, 0, len(revisions))
	seen := make(map[string]struct{}, len(revisions))
	activeIdx := -1

	if strings.TrimSpace(activeRevisionID) == "" && p.activeIdx >= 0 && p.activeIdx < len(p.revisions) {
		activeRevisionID = p.revisions[p.activeIdx].RevisionID
	}

	for i, rev := range revisions {
		revisionID := strings.TrimSpace(rev.RevisionID)
		if revisionID == "" {
			return fmt.Errorf("revisions[%d]: revision_id is required", i)
		}
		if _, ok := seen[revisionID]; ok {
			return fmt.Errorf("revisions[%d]: duplicate revision_id %q", i, revisionID)
		}
		seen[revisionID] = struct{}{}
		rev.RevisionID = revisionID

		clonedRev, err := cloneRevision(rev)
		if err != nil {
			return fmt.Errorf("revisions[%d]: %w", i, err)
		}
		cloned = append(cloned, clonedRev)
		if revisionID == activeRevisionID {
			activeIdx = i
		}
	}

	if strings.TrimSpace(activeRevisionID) != "" && activeIdx == -1 {
		return fmt.Errorf("active revision not found: %s", activeRevisionID)
	}

	p.revisions = cloned
	p.activeIdx = activeIdx
	p.syncPolicyFromActiveRevisionLocked()
	return p.persistStateLocked()
}

// UpsertRevision adds or replaces a single revision while preserving the rest of the ledger.
// When activate is true, the supplied revision becomes the active revision.
func (p *Publisher) UpsertRevision(revision Revision, activate bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	revisionID := strings.TrimSpace(revision.RevisionID)
	if revisionID == "" {
		return fmt.Errorf("revision_id is required")
	}
	revision.RevisionID = revisionID

	cloned, err := cloneRevision(revision)
	if err != nil {
		return err
	}

	if idx, existing := p.findRevisionLocked(revisionID); existing != nil {
		p.revisions[idx] = cloned
		if activate {
			p.activeIdx = idx
		}
	} else {
		p.revisions = append(p.revisions, cloned)
		if activate {
			p.activeIdx = len(p.revisions) - 1
		}
	}

	p.syncPolicyFromActiveRevisionLocked()
	return p.persistStateLocked()
}

// Publish publishes a configuration revision to gatewayd.
func (p *Publisher) Publish(revisionID string) (*PublishResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx, rev := p.findRevisionLocked(revisionID)
	if rev == nil {
		return nil, fmt.Errorf("revision not found: %s", revisionID)
	}

	result, err := p.publishRevisionLocked(rev, idx, "publish")
	if err != nil {
		return nil, err
	}

	log.Printf("[publisher] published revision %s as snapshot %s", revisionID, result.SnapshotID)

	return result, nil
}

// Rollback rolls back to a previous configuration revision.
func (p *Publisher) Rollback(revisionID string) (*PublishResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx, rev := p.findRevisionLocked(revisionID)
	if rev == nil {
		return nil, fmt.Errorf("revision not found: %s", revisionID)
	}

	// Publish the revision (rollback is just a publish with kind=rollback)
	result, err := p.publishRevisionLocked(rev, idx, "rollback")
	if err != nil {
		return nil, err
	}

	log.Printf("[publisher] rolled back to revision %s", revisionID)
	return result, nil
}

// publishRevisionLocked publishes a revision (must hold lock).
func (p *Publisher) publishRevisionLocked(rev *Revision, revisionIdx int, kind string) (*PublishResult, error) {
	compiled, err := p.compileSnapshotLocked(rev)
	if err != nil {
		return nil, err
	}

	snapshotID := generateSnapshotID()
	requestedAt := time.Now().UTC()
	publishedSnapshot, err := cloneSnapshot(compiled)
	if err != nil {
		return nil, fmt.Errorf("clone snapshot: %w", err)
	}
	publishedSnapshot.Meta.SnapshotID = snapshotID
	publishedSnapshot.Meta.RevisionID = rev.RevisionID
	if publishedSnapshot.Meta.SchemaVersion == 0 {
		publishedSnapshot.Meta.SchemaVersion = snapshot.CurrentSchemaVersion
	}
	if publishedSnapshot.Meta.GeneratedAt.IsZero() {
		publishedSnapshot.Meta.GeneratedAt = requestedAt
	}

	snapshotBytes, err := serializeSnapshot(publishedSnapshot)
	if err != nil {
		return nil, fmt.Errorf("serialize snapshot: %w", err)
	}

	publishID := generatePublishID()
	p.publishes = append(p.publishes, PublishRecord{
		PublishID:   publishID,
		RevisionID:  rev.RevisionID,
		SnapshotID:  snapshotID,
		RequestedAt: requestedAt,
		Kind:        kind,
		Status:      "staged",
	})
	p.trimPublishesLocked()
	pub := &p.publishes[len(p.publishes)-1]
	if err := p.persistStateLocked(); err != nil {
		log.Printf("[publisher] warning: could not persist staged publish state: %v", err)
	}

	req := gatewaycontrol.ApplySnapshotRequest{
		SnapshotID:    publishedSnapshot.Meta.SnapshotID,
		RevisionID:    rev.RevisionID,
		SnapshotBytes: snapshotBytes,
		SchemaVersion: publishedSnapshot.Meta.SchemaVersion,
		GeneratedAt:   publishedSnapshot.Meta.GeneratedAt,
	}

	if p.gateway == nil {
		pub.Status = "failed"
		pub.Error = "gateway not configured"
		if err := p.persistStateLocked(); err != nil {
			log.Printf("[publisher] warning: could not persist failed publish state: %v", err)
		}
		return nil, errors.New("gateway not configured")
	}

	resp, err := p.gateway.ApplySnapshot(req)
	if err != nil {
		pub.Status = "failed"
		pub.Error = err.Error()
		if persistErr := p.persistStateLocked(); persistErr != nil {
			log.Printf("[publisher] warning: could not persist failed publish state: %v", persistErr)
		}
		return &PublishResult{
			Success:      false,
			RevisionID:   rev.RevisionID,
			ErrorMessage: err.Error(),
		}, nil
	}

	if !resp.Applied {
		pub.Status = "failed"
		pub.Error = resp.Error
		if err := p.persistStateLocked(); err != nil {
			log.Printf("[publisher] warning: could not persist failed publish state: %v", err)
		}
		return &PublishResult{
			Success:      false,
			RevisionID:   rev.RevisionID,
			ErrorMessage: resp.Error,
		}, nil
	}

	pub.Status = "observed"
	pub.ObservedAt = time.Now()
	p.activeIdx = revisionIdx
	p.syncPolicyFromActiveRevisionLocked()
	if err := p.persistStateLocked(); err != nil {
		log.Printf("[publisher] warning: could not persist observed publish state: %v", err)
	}

	return &PublishResult{
		Success:     true,
		SnapshotID:  snapshotID,
		RevisionID:  rev.RevisionID,
		PublishedAt: pub.RequestedAt,
	}, nil
}

// GetCurrentConfig returns a cloned copy of the active revision config.
func (p *Publisher) GetCurrentConfig() (*core.Config, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.activeIdx < 0 || p.activeIdx >= len(p.revisions) {
		return nil, nil
	}
	return cloneConfig(p.revisions[p.activeIdx].Config)
}

// LoadRevisionConfig returns a cloned config payload for a stored revision.
func (p *Publisher) LoadRevisionConfig(revisionID string) (*core.Config, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	_, rev := p.findRevisionLocked(revisionID)
	if rev == nil {
		return nil, nil
	}
	if rev.Config == nil {
		return nil, fmt.Errorf("revision %s has no config payload", revisionID)
	}
	return cloneConfig(rev.Config)
}

func (p *Publisher) findRevisionLocked(revisionID string) (int, *Revision) {
	for i := range p.revisions {
		if p.revisions[i].RevisionID == revisionID {
			return i, &p.revisions[i]
		}
	}
	return -1, nil
}

func (p *Publisher) compileSnapshotLocked(rev *Revision) (*snapshot.Snapshot, error) {
	if rev.Snapshot != nil {
		return rev.Snapshot, nil
	}

	var (
		snap *snapshot.Snapshot
		err  error
	)

	if p.revisionCompiler != nil {
		compiledRevision, err := cloneRevision(*rev)
		if err != nil {
			return nil, fmt.Errorf("clone revision: %w", err)
		}
		snap, err = p.revisionCompiler(compiledRevision)
	} else {
		if p.compiler == nil {
			return nil, errors.New("compiler not configured")
		}
		if rev.Config == nil {
			return nil, fmt.Errorf("revision %s has no config payload", rev.RevisionID)
		}
		snap, err = p.compiler.CompileFromConfig(rev.Config)
	}
	if err != nil {
		return nil, fmt.Errorf("compile snapshot: %w", err)
	}
	if snap == nil {
		return nil, errors.New("compile snapshot: compiler returned nil snapshot")
	}

	rev.Snapshot, err = cloneSnapshot(snap)
	if err != nil {
		return nil, fmt.Errorf("clone snapshot: %w", err)
	}
	return rev.Snapshot, nil
}

// GetCurrentRevision returns the current active revision.
func (p *Publisher) GetCurrentRevision() (*RevisionInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.currentRevisionInfoLocked(), nil
}

// GetHistory returns the revision history.
func (p *Publisher) GetHistory(limit int) ([]RevisionInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	result := make([]RevisionInfo, 0, len(p.revisions))
	for i := len(p.revisions) - 1; i >= 0 && len(result) < limit; i-- {
		rev := &p.revisions[i]
		result = append(result, RevisionInfo{
			RevisionID:  rev.RevisionID,
			CreatedAt:   rev.CreatedAt,
			CreatedBy:   rev.CreatedBy,
			Description: rev.Description,
			IsActive:    i == p.activeIdx,
		})
	}

	return result, nil
}

// RevisionInfo contains revision information.
type RevisionInfo struct {
	RevisionID  string    `json:"revision_id"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
}

// PublishResult contains the result of a publish operation.
type PublishResult struct {
	Success      bool      `json:"success"`
	SnapshotID   string    `json:"snapshot_id"`
	RevisionID   string    `json:"revision_id"`
	PublishedAt  time.Time `json:"published_at"`
	ErrorMessage string    `json:"error,omitempty"`
}

// Helper functions

func generateSnapshotID() string {
	return "snap_" + time.Now().UTC().Format("20060102_150405") + "_" + randomSuffix()
}

func generatePublishID() string {
	return "pub_" + time.Now().UTC().Format("20060102_150405") + "_" + randomSuffix()
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[time.Now().Nanosecond()%len(chars)]
	}
	return string(b)
}

func serializeSnapshot(snap *snapshot.Snapshot) ([]byte, error) {
	// Snapshot credentials are intentionally omitted from JSON for API/display
	// safety, but the private control-plane RPC must deliver them to gatewayd.
	return yaml.Marshal(snap)
}

func cloneRevision(rev Revision) (Revision, error) {
	cloned := rev

	config, err := cloneConfig(rev.Config)
	if err != nil {
		return Revision{}, fmt.Errorf("clone config: %w", err)
	}
	cloned.Config = config

	snap, err := cloneSnapshot(rev.Snapshot)
	if err != nil {
		return Revision{}, fmt.Errorf("clone snapshot: %w", err)
	}
	cloned.Snapshot = snap

	return cloned, nil
}

func cloneConfig(cfg *core.Config) (*core.Config, error) {
	if cfg == nil {
		return nil, nil
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	var cloned core.Config
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func cloneSnapshot(snap *snapshot.Snapshot) (*snapshot.Snapshot, error) {
	if snap == nil {
		return nil, nil
	}

	data, err := yaml.Marshal(snap)
	if err != nil {
		return nil, err
	}

	var cloned snapshot.Snapshot
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (p *Publisher) restoreStateLocked(state PublisherState) error {
	revisions := make([]Revision, 0, len(state.Revisions))
	seen := make(map[string]struct{}, len(state.Revisions))
	activeIdx := -1

	for i, stored := range state.Revisions {
		rev, err := unmarshalStoredRevision(stored)
		if err != nil {
			return fmt.Errorf("restore revision[%d]: %w", i, err)
		}
		if rev.RevisionID == "" {
			return fmt.Errorf("restore revision[%d]: revision_id is required", i)
		}
		if _, ok := seen[rev.RevisionID]; ok {
			return fmt.Errorf("restore revision[%d]: duplicate revision_id %q", i, rev.RevisionID)
		}
		seen[rev.RevisionID] = struct{}{}
		revisions = append(revisions, rev)
		if rev.RevisionID == strings.TrimSpace(state.ActiveRevisionID) {
			activeIdx = i
		}
	}

	activeRevisionID := strings.TrimSpace(state.ActiveRevisionID)
	if activeRevisionID != "" && activeIdx == -1 {
		return fmt.Errorf("restore active revision not found: %s", activeRevisionID)
	}

	p.revisions = revisions
	p.activeIdx = activeIdx
	p.syncPolicyFromActiveRevisionLocked()
	p.publishes = append([]PublishRecord(nil), state.Publishes...)
	p.trimPublishesLocked()
	return nil
}

func (p *Publisher) persistStateLocked() error {
	if p.stateStore == nil {
		return nil
	}

	state := &PublisherState{
		Version:   currentStateVersion,
		Revisions: make([]StoredRevision, 0, len(p.revisions)),
		Publishes: append([]PublishRecord(nil), p.publishes...),
	}
	if p.activeIdx >= 0 && p.activeIdx < len(p.revisions) {
		state.ActiveRevisionID = p.revisions[p.activeIdx].RevisionID
	}
	for i := range p.revisions {
		stored, err := marshalStoredRevision(p.revisions[i])
		if err != nil {
			return fmt.Errorf("persist revision[%d]: %w", i, err)
		}
		state.Revisions = append(state.Revisions, stored)
	}
	return p.stateStore.Save(state)
}

func (p *Publisher) trimPublishesLocked() {
	limit := p.policy.PublishHistoryLimit
	if limit <= 0 || len(p.publishes) <= limit {
		return
	}
	p.publishes = p.publishes[len(p.publishes)-limit:]
}

func (p *Publisher) currentRevisionInfoLocked() *RevisionInfo {
	if p.activeIdx < 0 || p.activeIdx >= len(p.revisions) {
		return nil
	}

	rev := &p.revisions[p.activeIdx]
	return &RevisionInfo{
		RevisionID:  rev.RevisionID,
		CreatedAt:   rev.CreatedAt,
		CreatedBy:   rev.CreatedBy,
		Description: rev.Description,
		IsActive:    true,
	}
}

func (p *Publisher) syncPolicyFromActiveRevisionLocked() {
	if p.activeIdx < 0 || p.activeIdx >= len(p.revisions) {
		return
	}

	cfg := p.revisions[p.activeIdx].Config
	if cfg == nil {
		return
	}

	p.policy = PublisherPolicyFromConfig(cfg)
	p.trimPublishesLocked()
}

// NormalizePublisherPolicy applies defaults for zero-value fields.
func NormalizePublisherPolicy(policy PublisherPolicy) PublisherPolicy {
	if policy.PublishHistoryLimit <= 0 {
		policy.PublishHistoryLimit = core.DefaultAdminPublishHistoryLimit
	}
	return policy
}

// PublisherPolicyFromConfig derives publisher runtime policy from the active config.
func PublisherPolicyFromConfig(cfg *core.Config) PublisherPolicy {
	if cfg == nil {
		return NormalizePublisherPolicy(PublisherPolicy{})
	}
	return NormalizePublisherPolicy(PublisherPolicy{
		PublishHistoryLimit: cfg.Admin.PublishHistoryLimit,
	})
}

// ValidateConfig validates a configuration without publishing it.
func (p *Publisher) ValidateConfig(cfg interface{}) (*ConfigValidationResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.compiler == nil {
		return nil, errors.New("compiler not configured")
	}

	coreCfg, err := asCoreConfig(cfg)
	if err != nil {
		return &ConfigValidationResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	// CompileFromConfig already performs validation
	snap, err := p.compiler.CompileFromConfig(coreCfg)
	if err != nil {
		return &ConfigValidationResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	// Additional snapshot validation
	if err := p.compiler.Validate(snap); err != nil {
		return &ConfigValidationResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	return &ConfigValidationResult{
		Valid:    true,
		Warnings: []string{},
	}, nil
}

// UpdateConfig creates a new revision and publishes it.
func (p *Publisher) UpdateConfig(cfg interface{}, description string) (*PublishResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.compiler == nil {
		return nil, errors.New("compiler not configured")
	}

	coreCfg, err := asCoreConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Compile and validate the config
	snap, err := p.compiler.CompileFromConfig(coreCfg)
	if err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	if err := p.compiler.Validate(snap); err != nil {
		return nil, fmt.Errorf("validate snapshot: %w", err)
	}

	// Create a new revision
	revisionID := "rev_" + time.Now().UTC().Format("20060102_150405") + "_" + randomSuffix()
	now := time.Now().UTC()
	newRevision := Revision{
		RevisionID:  revisionID,
		CreatedAt:   now,
		CreatedBy:   "admin-ui",
		Description: description,
		Config:      coreCfg,
		Snapshot:    snap,
	}

	// Add to revisions list
	p.revisions = append(p.revisions, newRevision)
	newIdx := len(p.revisions) - 1

	// Publish the new revision
	result, err := p.publishRevisionLocked(&p.revisions[newIdx], newIdx, "publish")
	if err != nil {
		// Remove the failed revision
		p.revisions = p.revisions[:newIdx]
		return nil, err
	}

	if err := p.persistStateLocked(); err != nil {
		log.Printf("[publisher] warning: could not persist state after update: %v", err)
	}

	log.Printf("[publisher] updated config as revision %s", revisionID)
	return result, nil
}

// ConfigValidationResult contains the result of config validation.
type ConfigValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func asCoreConfig(cfg interface{}) (*core.Config, error) {
	switch typed := cfg.(type) {
	case core.Config:
		return &typed, nil
	case *core.Config:
		if typed == nil {
			return nil, errors.New("config is nil")
		}
		return typed, nil
	case map[string]interface{}:
		// Convert map to YAML then to core.Config
		data, err := yaml.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal config: %w", err)
		}
		var coreCfg core.Config
		if err := yaml.Unmarshal(data, &coreCfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		return &coreCfg, nil
	default:
		return nil, fmt.Errorf("unsupported config type %T; expected core.Config, *core.Config, or map", cfg)
	}
}
