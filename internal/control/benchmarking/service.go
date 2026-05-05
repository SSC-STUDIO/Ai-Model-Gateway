package benchmarking

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/logger"

	"github.com/google/uuid"
)

// ConfigSource exposes the active control-plane config.
type ConfigSource interface {
	GetCurrentConfig() (*core.Config, error)
}

// GatewayRunner executes synthetic benchmark cases through gatewayd.
type GatewayRunner interface {
	RunBenchmarkCase(req gatewaycontrol.RunBenchmarkCaseRequest) (*gatewaycontrol.RunBenchmarkCaseResponse, error)
}

// Service orchestrates verification benchmark runs.
type Service struct {
	store   *Store
	configs ConfigSource
	gateway GatewayRunner

	mu         sync.Mutex
	activeRuns int
}

// NewService constructs a verification benchmark service.
func NewService(store *Store, configs ConfigSource, gateway GatewayRunner) *Service {
	return &Service{
		store:   store,
		configs: configs,
		gateway: gateway,
	}
}

// ListBaselineSnapshots returns all imported baseline snapshots.
func (s *Service) ListBaselineSnapshots(ctx context.Context) ([]BaselineSnapshot, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.ListBaselineSnapshots(ctx)
}

// ImportBaseline imports one immutable baseline snapshot from JSON or CSV.
func (s *Service) ImportBaseline(ctx context.Context, req ImportBaselineRequest) (*BaselineSnapshot, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("benchmark store not configured")
	}
	kind := strings.TrimSpace(req.Kind)
	switch kind {
	case BaselineKindPublicStandard, BaselineKindVendorClaim:
	default:
		return nil, fmt.Errorf("invalid baseline kind: %s", req.Kind)
	}
	rows, err := parseBaselineRows(req.FileName, req.Contents)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("baseline import did not contain any rows")
	}
	now := time.Now().UTC()
	capturedAt := now
	if req.CapturedAt != nil && !req.CapturedAt.IsZero() {
		capturedAt = req.CapturedAt.UTC()
	}
	snapshot := BaselineSnapshot{
		SnapshotID: "baseline_" + uuid.New().String(),
		Kind:       kind,
		SourceName: strings.TrimSpace(req.SourceName),
		SourceURL:  strings.TrimSpace(req.SourceURL),
		CapturedAt: capturedAt,
		ImportedAt: now,
	}
	if snapshot.SourceName == "" {
		snapshot.SourceName = strings.TrimSpace(req.FileName)
	}
	return s.store.InsertBaselineSnapshot(ctx, snapshot, rows)
}

// ListRuns returns recent benchmark runs.
func (s *Service) ListRuns(ctx context.Context, limit int) ([]RunSummary, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.ListRuns(ctx, limit)
}

// GetRun returns one benchmark run with all target results.
func (s *Service) GetRun(ctx context.Context, runID string) (*RunDetail, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.GetRun(ctx, strings.TrimSpace(runID))
}

// StartRun creates and starts a verification benchmark run.
func (s *Service) StartRun(ctx context.Context, req StartRunRequest) (*RunDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("benchmark store not configured")
	}
	if s.gateway == nil {
		return nil, errors.New("gateway benchmark runner not connected")
	}
	cfg, err := s.currentConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Benchmarking.Enabled {
		return nil, errors.New("benchmarking is disabled")
	}
	if err := s.validateBaselineSelection(ctx, strings.TrimSpace(req.PublicSnapshotID), BaselineKindPublicStandard); err != nil {
		return nil, err
	}
	if err := s.validateBaselineSelection(ctx, strings.TrimSpace(req.VendorSnapshotID), BaselineKindVendorClaim); err != nil {
		return nil, err
	}
	suiteName := strings.TrimSpace(req.Suite)
	if suiteName == "" {
		suiteName = cfg.Benchmarking.DefaultSuite
	}
	suite, err := suiteByName(suiteName)
	if err != nil {
		return nil, err
	}
	requestedProtocol := strings.TrimSpace(req.Protocol)
	protocol := normalizeProtocol(requestedProtocol)
	if requestedProtocol == "" || strings.EqualFold(requestedProtocol, "auto") {
		protocol = "auto"
	}
	targets, err := expandTargets(cfg, req, suite.Name)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("no benchmark targets resolved")
	}

	maxParallelRuns := cfg.Benchmarking.Limits.MaxParallelRuns
	if maxParallelRuns <= 0 {
		maxParallelRuns = 1
	}

	s.mu.Lock()
	if s.activeRuns >= maxParallelRuns {
		s.mu.Unlock()
		return nil, fmt.Errorf("too many active benchmark runs: %d", s.activeRuns)
	}
	s.activeRuns++
	s.mu.Unlock()

	now := time.Now().UTC()
	run := RunSummary{
		RunID:            "run_" + uuid.New().String(),
		Status:           RunStatusRunning,
		SuiteVersion:     suite.Name,
		Protocol:         protocol,
		PublicSnapshotID: strings.TrimSpace(req.PublicSnapshotID),
		VendorSnapshotID: strings.TrimSpace(req.VendorSnapshotID),
		StartedAt:        now,
		TargetCount:      len(targets),
		CompletedTargets: 0,
	}
	for i := range targets {
		targets[i].TargetID = "target_" + uuid.New().String()
		targets[i].RunID = run.RunID
		targets[i].Status = TargetStatusQueued
		targets[i].StartedAt = now
	}
	if err := s.store.CreateRun(ctx, run, targets); err != nil {
		s.mu.Lock()
		s.activeRuns--
		s.mu.Unlock()
		return nil, err
	}

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.executeRun(ctx, run, suite, cfg, targets)
	}()
	return s.store.GetRun(ctx, run.RunID)
}

func (s *Service) executeRun(ctx context.Context, run RunSummary, suite *benchmarkSuite, cfg *core.Config, targets []RunTargetDetail) {
	defer func() {
		s.mu.Lock()
		if s.activeRuns > 0 {
			s.activeRuns--
		}
		s.mu.Unlock()
	}()

	runStatus := RunStatusCompleted
	runError := ""

	for _, target := range targets {
		select {
		case <-ctx.Done():
			runStatus = RunStatusCancelled
			return
		default:
		}
		result := s.executeTarget(ctx, cfg, suite, target, run)
		if err := s.store.UpdateTarget(ctx, result); err != nil && runError == "" {
			runError = err.Error()
			runStatus = RunStatusFailed
		}
		if result.Status != TargetStatusCompleted && runStatus == RunStatusCompleted {
			runStatus = RunStatusIncomplete
		}
	}

	if runError != "" {
		runStatus = RunStatusFailed
	}
	if err := s.store.UpdateRunStatus(ctx, run.RunID, runStatus, runError, time.Now().UTC()); err != nil {
		logger.Error("failed to update benchmark run status",
			"run_id", run.RunID,
			"status", runStatus,
			"error", err,
		)
	}
}

func (s *Service) executeTarget(ctx context.Context, cfg *core.Config, suite *benchmarkSuite, target RunTargetDetail, run RunSummary) RunTargetDetail {
	target.Status = TargetStatusRunning
	target.PublicSnapshotID = run.PublicSnapshotID
	target.VendorSnapshotID = run.VendorSnapshotID
	target.JudgeModel = cfg.Benchmarking.Judge.PublicModel
	target.Cases = make([]RunCaseResult, len(suite.Cases))

	judge, judgeErr := s.buildJudgeFunc(ctx, cfg, target)
	if judgeErr != nil {
		target.Status = TargetStatusIncomplete
		target.Verdict = VerdictIncomplete
		target.CompletedAt = time.Now().UTC()
		target.Error = judgeErr.Error()
		target.ReasonCodes = []string{"judge_unavailable"}
		target.CompletionRate = 0
		return target
	}

	maxParallelCases := cfg.Benchmarking.Limits.MaxParallelCases
	if maxParallelCases <= 0 {
		maxParallelCases = 1
	}
	type caseWorkResult struct {
		index int
		item  RunCaseResult
		resp  *gatewaycontrol.RunBenchmarkCaseResponse
	}
	results := make(chan caseWorkResult, len(suite.Cases))
	sem := make(chan struct{}, maxParallelCases)
	var wg sync.WaitGroup

	for i, def := range suite.Cases {
		i := i
		def := def
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item, resp := s.executeCase(ctx, cfg, suite, target, def, run, judge)
			results <- caseWorkResult{index: i, item: item, resp: resp}
		}()
	}
	wg.Wait()
	close(results)

	completedCases := 0
	criticalFailures := 0
	dimensionCaseScores := make(map[string][]float64)
	reasonCodes := make([]string, 0, 8)

	for result := range results {
		item := result.item
		item.CaseID = suite.Cases[result.index].ID
		item.Dimension = suite.Cases[result.index].Dimension
		item.Kind = suite.Cases[result.index].Kind
		item.Critical = suite.Cases[result.index].Critical
		target.Cases[result.index] = item
		if result.resp != nil && target.EffectiveModel == "" {
			target.EffectiveModel = result.resp.EffectiveModel
		}
		target.PromptTokens += item.PromptTokens
		target.CachedPromptTokens += item.CachedPromptTokens
		target.CompletionTokens += item.CompletionTokens
		target.EstimatedCostUSD += item.CostUSD
		if item.Completed {
			completedCases++
		}
		dimensionCaseScores[item.Dimension] = append(dimensionCaseScores[item.Dimension], item.Score)
		if item.Critical && item.Score < 100 {
			criticalFailures++
			reasonCodes = append(reasonCodes, fmt.Sprintf("critical_%s_failed", item.Kind))
		}
		if item.Error != "" {
			reasonCodes = append(reasonCodes, fmt.Sprintf("case_%s_error", item.CaseID))
		}
	}

	target.DimensionScores = make(map[string]float64, len(suite.DimensionWeights))
	for dimension := range suite.DimensionWeights {
		scores := dimensionCaseScores[dimension]
		if len(scores) == 0 {
			target.DimensionScores[dimension] = 0
			continue
		}
		total := 0.0
		for _, score := range scores {
			total += score
		}
		target.DimensionScores[dimension] = total / float64(len(scores))
	}
	target.OverallScore = weightedAverage(target.DimensionScores, suite.DimensionWeights)
	target.CriticalProtocolFailures = criticalFailures
	target.CompletionRate = float64(completedCases) * 100 / float64(len(suite.Cases))
	target.ReasonCodes = uniqueStrings(reasonCodes)

	canonicalModelID, resolveErr := s.resolveCanonicalModelID(ctx, run, target)
	if resolveErr != nil {
		target.Status = TargetStatusIncomplete
		target.Verdict = VerdictIncomplete
		target.Error = resolveErr.Error()
		target.CompletedAt = time.Now().UTC()
		target.ReasonCodes = uniqueStrings(append(target.ReasonCodes, "canonical_model_unresolved"))
		return target
	}
	target.CanonicalModelID = canonicalModelID

	publicGap, publicFound := s.compareAgainstBaseline(ctx, run.PublicSnapshotID, canonicalModelID, target.DimensionScores, suite.DimensionWeights)
	vendorGap, vendorFound := s.compareAgainstBaseline(ctx, run.VendorSnapshotID, canonicalModelID, target.DimensionScores, suite.DimensionWeights)
	target.PublicGap = publicGap
	target.VendorGap = vendorGap
	if run.PublicSnapshotID != "" && !publicFound {
		target.ReasonCodes = uniqueStrings(append(target.ReasonCodes, "public_baseline_missing_for_model"))
	}
	if run.VendorSnapshotID != "" && !vendorFound {
		target.ReasonCodes = uniqueStrings(append(target.ReasonCodes, "vendor_baseline_missing_for_model"))
	}

	baselineSelected := run.PublicSnapshotID != "" || run.VendorSnapshotID != ""
	verdict, suspicion, reasons := verdictForTarget(target, cfg.Benchmarking.VerdictThresholds, publicFound, vendorFound, baselineSelected)
	target.Verdict = verdict
	target.SuspicionScore = suspicion
	target.ReasonCodes = uniqueStrings(append(target.ReasonCodes, reasons...))
	target.CompletedAt = time.Now().UTC()
	switch verdict {
	case VerdictIncomplete:
		target.Status = TargetStatusIncomplete
	default:
		target.Status = TargetStatusCompleted
	}
	return target
}

func (s *Service) executeCase(
	ctx context.Context,
	cfg *core.Config,
	suite *benchmarkSuite,
	target RunTargetDetail,
	def benchmarkCaseDefinition,
	run RunSummary,
	judge judgeFunc,
) (RunCaseResult, *gatewaycontrol.RunBenchmarkCaseResponse) {
	body, err := def.build(target.Protocol, target.PublicModel)
	if err != nil {
		return RunCaseResult{
			Completed: false,
			Error:     err.Error(),
			Reason:    "case_build_failed",
		}, nil
	}
	timeout := cfg.Benchmarking.Limits.PerCaseTimeoutMs
	if timeout <= 0 {
		timeout = 30000
	}
	resp, err := s.gateway.RunBenchmarkCase(gatewaycontrol.RunBenchmarkCaseRequest{
		RunID:             run.RunID,
		CaseID:            def.ID,
		BenchmarkTargetID: target.TargetID,
		ProviderID:        target.ProviderID,
		PublicModel:       target.PublicModel,
		Protocol:          target.Protocol,
		RequestBody:       body,
		TimeoutMs:         timeout,
		SyntheticKind:     "benchmark",
		DisableCache:      true,
		DisableFallback:   true,
		DisableRetries:    true,
	})
	if err != nil {
		return RunCaseResult{
			Completed: false,
			Error:     err.Error(),
			Reason:    "gateway_rpc_failed",
		}, nil
	}
	item := def.score(ctx, target.Protocol, resp, judge)
	return item, resp
}

func (s *Service) buildJudgeFunc(ctx context.Context, cfg *core.Config, target RunTargetDetail) (judgeFunc, error) {
	judgeModel := strings.TrimSpace(cfg.Benchmarking.Judge.PublicModel)
	if judgeModel == "" {
		return nil, errors.New("benchmark judge model is not configured")
	}
	judgeProvider, err := resolveJudgeProvider(cfg, judgeModel, target.ProviderID)
	if err != nil {
		return nil, err
	}
	if judgeProvider == target.ProviderID && strings.EqualFold(strings.TrimSpace(judgeModel), strings.TrimSpace(target.PublicModel)) {
		return nil, errors.New("judge route cannot be the same as the benchmark target route")
	}
	timeout := cfg.Benchmarking.Judge.TimeoutMs
	if timeout <= 0 {
		timeout = 30000
	}
	return func(ctx context.Context, prompt judgePrompt) (float64, string, error) {
		body, err := buildRequest(ProtocolOpenAIChat, judgeModel, false,
			"You are a strict benchmark judge. Return JSON only.",
			fmt.Sprintf(
				"Rubric:\n%s\n\nCandidate response:\n%s\n\nReturn JSON: {\"score\": 0-100, \"reason\": \"short explanation\"}",
				strings.TrimSpace(prompt.Rubric),
				strings.TrimSpace(prompt.Response),
			),
			nil,
		)
		if err != nil {
			return 0, "", err
		}
		resp, err := s.gateway.RunBenchmarkCase(gatewaycontrol.RunBenchmarkCaseRequest{
			RunID:             "judge_" + target.TargetID,
			CaseID:            "judge_" + uuid.New().String(),
			BenchmarkTargetID: target.TargetID,
			ProviderID:        judgeProvider,
			PublicModel:       judgeModel,
			Protocol:          ProtocolOpenAIChat,
			RequestBody:       body,
			TimeoutMs:         timeout,
			SyntheticKind:     "benchmark",
			DisableCache:      true,
			DisableFallback:   true,
			DisableRetries:    true,
		})
		if err != nil {
			return 0, "", err
		}
		if resp == nil {
			return 0, "", errors.New("empty judge response")
		}
		if resp.Error != "" {
			return 0, "", errors.New(resp.Error)
		}
		answer := extractAssistantText(ProtocolOpenAIChat, resp.ResponseBody)
		var payload struct {
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(answer)), &payload); err != nil {
			return 0, "", fmt.Errorf("parse judge response: %w", err)
		}
		return clampScore(payload.Score), strings.TrimSpace(payload.Reason), nil
	}, nil
}

func (s *Service) compareAgainstBaseline(
	ctx context.Context,
	snapshotID string,
	canonicalModelID string,
	dimensionScores map[string]float64,
	dimensionWeights map[string]float64,
) (float64, bool) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return 0, false
	}
	rows, err := s.store.ListBaselineRows(ctx, snapshotID, canonicalModelID)
	if err != nil || len(rows) == 0 {
		return 0, false
	}
	baselineScores := make(map[string]float64, len(rows))
	for _, row := range rows {
		scaleMax := row.ScaleMax
		if scaleMax <= 0 {
			scaleMax = 100
		}
		baselineScores[row.MetricName] = clampScore(row.Score / scaleMax * 100)
	}
	if overallScore, ok := baselineScores["overall"]; ok {
		return absFloat(weightedAverage(dimensionScores, dimensionWeights) - overallScore), true
	}
	weightedGap := 0.0
	weightSum := 0.0
	for dimension, weight := range dimensionWeights {
		baselineScore, ok := baselineScores[dimension]
		if !ok {
			continue
		}
		weightedGap += absFloat(dimensionScores[dimension]-baselineScore) * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0, false
	}
	return weightedGap / weightSum, true
}

func (s *Service) resolveCanonicalModelID(ctx context.Context, run RunSummary, target RunTargetDetail) (string, error) {
	cfg, err := s.currentConfig()
	if err != nil {
		return "", err
	}
	candidates := uniqueStrings([]string{target.EffectiveModel, target.PublicModel})
	for _, alias := range cfg.Benchmarking.Aliases {
		if alias.Provider != "" && !strings.EqualFold(alias.Provider, target.ProviderID) {
			continue
		}
		for _, candidate := range candidates {
			for _, model := range alias.Models {
				if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(candidate)) {
					return alias.CanonicalModelID, nil
				}
			}
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		for _, snapshotID := range []string{run.PublicSnapshotID, run.VendorSnapshotID} {
			if snapshotID == "" {
				continue
			}
			rows, err := s.store.ListBaselineRows(ctx, snapshotID, candidate)
			if err == nil && len(rows) > 0 {
				return candidate, nil
			}
		}
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no canonical model candidate found for provider=%s model=%s effective_model=%s", target.ProviderID, target.PublicModel, target.EffectiveModel)
}

func (s *Service) currentConfig() (*core.Config, error) {
	if s.configs == nil {
		return nil, errors.New("benchmark config source not configured")
	}
	cfg, err := s.configs.GetCurrentConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("no active config available")
	}
	cfg.Normalize()
	return cfg, nil
}

func expandTargets(cfg *core.Config, req StartRunRequest, suiteName string) ([]RunTargetDetail, error) {
	if cfg == nil {
		return nil, errors.New("no config available")
	}
	requestedProtocol := strings.TrimSpace(req.Protocol)
	providers := make(map[string]core.Provider, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if !provider.IsEnabled() {
			continue
		}
		providers[provider.Name] = provider
	}
	addTarget := func(items []RunTargetDetail, provider core.Provider, publicModel string) []RunTargetDetail {
		protocol := ""
		if requestedProtocol != "" {
			protocol = normalizeProtocol(requestedProtocol)
		}
		if protocol == "" {
			protocol = normalizeProtocol(provider.ProtocolAdapter)
		}
		if protocol == "" {
			protocol = ProtocolOpenAIChat
		}
		return append(items, RunTargetDetail{
			Status:          TargetStatusQueued,
			ProviderID:      provider.Name,
			PublicModel:     publicModel,
			Protocol:        protocol,
			ProtocolAdapter: provider.ProtocolAdapter,
			SuiteVersion:    suiteName,
		})
	}

	targets := make([]RunTargetDetail, 0, len(cfg.Providers))
	if req.AllActive {
		seen := make(map[string]struct{})
		for _, provider := range cfg.Providers {
			if !provider.IsEnabled() {
				continue
			}
			for _, model := range provider.Models {
				key := provider.Name + "\x00" + model
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				targets = addTarget(targets, provider, model)
			}
		}
		return targets, nil
	}

	providerID := strings.TrimSpace(req.ProviderID)
	publicModel := strings.TrimSpace(req.PublicModel)
	if providerID == "" || publicModel == "" {
		return nil, errors.New("provider_id and public_model are required unless all_active=true")
	}
	provider, ok := providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}
	found := false
	for _, model := range provider.Models {
		if strings.EqualFold(model, publicModel) {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("provider %s does not advertise public model %s", providerID, publicModel)
	}
	targets = addTarget(targets, provider, publicModel)
	return targets, nil
}

func resolveJudgeProvider(cfg *core.Config, judgePublicModel string, targetProviderID string) (string, error) {
	if cfg == nil {
		return "", errors.New("no config available")
	}
	preferred := strings.TrimSpace(cfg.Benchmarking.Judge.Provider)
	if preferred != "" {
		for _, provider := range cfg.Providers {
			if !provider.IsEnabled() {
				continue
			}
			if provider.Name != preferred {
				continue
			}
			for _, model := range provider.Models {
				if strings.EqualFold(model, judgePublicModel) {
					return provider.Name, nil
				}
			}
			return "", fmt.Errorf("judge provider %s does not serve model %s", preferred, judgePublicModel)
		}
		return "", fmt.Errorf("judge provider not found: %s", preferred)
	}
	for _, provider := range cfg.Providers {
		if !provider.IsEnabled() {
			continue
		}
		if provider.Name == targetProviderID {
			continue
		}
		for _, model := range provider.Models {
			if strings.EqualFold(model, judgePublicModel) {
				return provider.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no judge provider found for model %s outside provider %s", judgePublicModel, targetProviderID)
}

func parseBaselineRows(fileName, contents string) ([]BaselineMetricRow, error) {
	trimmed := strings.TrimSpace(contents)
	if trimmed == "" {
		return nil, errors.New("baseline contents are empty")
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".csv") {
		return parseBaselineCSV(strings.NewReader(trimmed))
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		return parseBaselineJSON([]byte(trimmed))
	}
	return parseBaselineCSV(strings.NewReader(trimmed))
}

func parseBaselineJSON(data []byte) ([]BaselineMetricRow, error) {
	var direct []BaselineMetricRow
	if err := json.Unmarshal(data, &direct); err == nil && len(direct) > 0 {
		return normalizeBaselineRows(direct)
	}
	var wrapped struct {
		Rows []BaselineMetricRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("parse baseline json: %w", err)
	}
	return normalizeBaselineRows(wrapped.Rows)
}

func parseBaselineCSV(r io.Reader) ([]BaselineMetricRow, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read baseline csv header: %w", err)
	}
	index := make(map[string]int, len(header))
	for i, name := range header {
		index[strings.ToLower(strings.TrimSpace(name))] = i
	}
	rows := make([]BaselineMetricRow, 0, 32)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read baseline csv: %w", err)
		}
		row := BaselineMetricRow{
			CanonicalModelID: field(record, index, "canonical_model_id"),
			SourceModelName:  field(record, index, "source_model_name"),
			Family:           field(record, index, "family"),
			MetricName:       field(record, index, "metric_name"),
		}
		scoreRaw := field(record, index, "score")
		if strings.TrimSpace(scoreRaw) == "" {
			return nil, fmt.Errorf("baseline csv row missing score for model %s metric %s", row.CanonicalModelID, row.MetricName)
		}
		row.Score, err = strconv.ParseFloat(scoreRaw, 64)
		if err != nil {
			return nil, fmt.Errorf("parse baseline csv score %q for model %s metric %s: %w", scoreRaw, row.CanonicalModelID, row.MetricName, err)
		}
		scaleRaw := field(record, index, "scale_max")
		if strings.TrimSpace(scaleRaw) != "" {
			row.ScaleMax, err = strconv.ParseFloat(scaleRaw, 64)
			if err != nil {
				return nil, fmt.Errorf("parse baseline csv scale_max %q for model %s metric %s: %w", scaleRaw, row.CanonicalModelID, row.MetricName, err)
			}
		}
		if metadata := field(record, index, "metadata_json"); metadata != "" {
			_ = json.Unmarshal([]byte(metadata), &row.Metadata)
		}
		rows = append(rows, row)
	}
	return normalizeBaselineRows(rows)
}

func normalizeBaselineRows(rows []BaselineMetricRow) ([]BaselineMetricRow, error) {
	normalized := make([]BaselineMetricRow, 0, len(rows))
	for i, row := range rows {
		row.CanonicalModelID = strings.TrimSpace(row.CanonicalModelID)
		row.SourceModelName = strings.TrimSpace(row.SourceModelName)
		row.Family = strings.TrimSpace(row.Family)
		row.MetricName = strings.TrimSpace(row.MetricName)
		if row.CanonicalModelID == "" {
			return nil, fmt.Errorf("baseline row %d missing canonical_model_id", i)
		}
		if row.MetricName == "" {
			return nil, fmt.Errorf("baseline row %d missing metric_name", i)
		}
		if row.ScaleMax <= 0 {
			row.ScaleMax = 100
		}
		normalized = append(normalized, row)
	}
	return normalized, nil
}

func verdictForTarget(target RunTargetDetail, thresholds core.BenchmarkVerdictThresholds, publicFound, vendorFound, baselineSelected bool) (string, float64, []string) {
	if target.CompletionRate < 80 {
		return VerdictIncomplete, clampScore(maxGap(target.PublicGap, target.VendorGap) + float64(100-target.CompletionRate)/2), []string{"completion_rate_below_80"}
	}
	if baselineSelected && !publicFound && !vendorFound {
		return VerdictIncomplete, clampScore(float64(100-target.CompletionRate) / 2), []string{"no_baseline_rows_for_target"}
	}
	reasons := make([]string, 0, 8)
	maxObservedGap := 0.0
	if publicFound {
		maxObservedGap = maxGap(maxObservedGap, target.PublicGap)
		if target.PublicGap > thresholds.NormalMaxGap {
			reasons = append(reasons, "public_gap_above_normal")
		}
		if target.PublicGap > thresholds.SuspectMaxGap {
			reasons = append(reasons, "public_gap_above_suspect")
		}
	}
	if vendorFound {
		maxObservedGap = maxGap(maxObservedGap, target.VendorGap)
		if target.VendorGap > thresholds.NormalMaxGap {
			reasons = append(reasons, "vendor_gap_above_normal")
		}
		if target.VendorGap > thresholds.SuspectMaxGap {
			reasons = append(reasons, "vendor_gap_above_suspect")
		}
	}

	protocolPenalty := float64(target.CriticalProtocolFailures) * 20
	suspicion := clampScore(maxObservedGap + protocolPenalty + maxFloat(0, (100-target.CompletionRate)/2))
	switch {
	case target.CriticalProtocolFailures >= thresholds.HighSuspectProtocolFailures || maxObservedGap > thresholds.SuspectMaxGap:
		return VerdictHighSuspect, suspicion, reasons
	case target.CriticalProtocolFailures == 1 || maxObservedGap > thresholds.NormalMaxGap:
		return VerdictSuspect, suspicion, reasons
	default:
		return VerdictNormal, suspicion, reasons
	}
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "auto":
		return ""
	case ProtocolOpenAIChat:
		return ProtocolOpenAIChat
	case ProtocolAnthropicMessage:
		return ProtocolAnthropicMessage
	default:
		return strings.TrimSpace(protocol)
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func weightedAverage(values map[string]float64, weights map[string]float64) float64 {
	if len(weights) == 0 {
		return 0
	}
	total := 0.0
	weightSum := 0.0
	for key, weight := range weights {
		total += values[key] * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return 0
	}
	return total / weightSum
}

func maxGap(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func field(record []string, index map[string]int, key string) string {
	pos, ok := index[key]
	if !ok || pos < 0 || pos >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[pos])
}

func (s *Service) validateBaselineSelection(ctx context.Context, snapshotID string, expectedKind string) error {
	if snapshotID == "" {
		return nil
	}
	snapshot, err := s.store.GetBaselineSnapshot(ctx, snapshotID)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return fmt.Errorf("baseline snapshot not found: %s", snapshotID)
	}
	if expectedKind != "" && snapshot.Kind != expectedKind {
		return fmt.Errorf("baseline snapshot %s has kind %s, want %s", snapshotID, snapshot.Kind, expectedKind)
	}
	return nil
}
