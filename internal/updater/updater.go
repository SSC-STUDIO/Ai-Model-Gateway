// Package updater provides release discovery, bundle download, verification,
// and local payload replacement for AI Model Gateway.
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/release"
	"ai-model-gateway/internal/version"
)

const (
	DefaultRepository = "SSC-STUDIO/Ai-Model-Gateway"
	DefaultAPIBaseURL = "https://api.github.com"
	stateFileName     = "update-status.json"
)

// Options controls release discovery and local update paths.
type Options struct {
	CurrentVersion string
	Repository     string
	APIBaseURL     string
	Platform       string
	DownloadDir    string
	InstallDir     string
	StateDir       string
	HTTPClient     *http.Client
}

// Asset describes a GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type,omitempty"`
}

// ReleaseInfo describes the subset of GitHub release metadata used for updates.
type ReleaseInfo struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name,omitempty"`
	Body        string    `json:"body,omitempty"`
	HTMLURL     string    `json:"html_url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Prerelease  bool      `json:"prerelease,omitempty"`
	Draft       bool      `json:"draft,omitempty"`
	Assets      []Asset   `json:"assets"`
}

// CheckResult is a point-in-time online release check.
type CheckResult struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	LatestTag       string    `json:"latest_tag,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	Platform        string    `json:"platform"`
	Repository      string    `json:"repository"`
	AssetName       string    `json:"asset_name,omitempty"`
	AssetURL        string    `json:"asset_url,omitempty"`
	AssetSize       int64     `json:"asset_size,omitempty"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
	Prerelease      bool      `json:"prerelease,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
	Message         string    `json:"message,omitempty"`
}

// FetchResult describes a downloaded and verified update bundle.
type FetchResult struct {
	CheckResult
	ArchivePath string               `json:"archive_path,omitempty"`
	BundleDir   string               `json:"bundle_dir,omitempty"`
	Manifest    release.Manifest     `json:"manifest,omitempty"`
	Verify      release.VerifyReport `json:"verify"`
	Downloaded  bool                 `json:"downloaded"`
}

// ApplyOptions controls local payload replacement.
type ApplyOptions struct {
	BundleRoot string
	InstallDir string
	StateDir   string
	DryRun     bool
	Now        time.Time
}

// ApplyResult describes a local update apply attempt.
type ApplyResult struct {
	Applied        bool                 `json:"applied"`
	DryRun         bool                 `json:"dry_run"`
	ProductVersion string               `json:"product_version,omitempty"`
	BundleRoot     string               `json:"bundle_root,omitempty"`
	InstallDir     string               `json:"install_dir,omitempty"`
	BackupDir      string               `json:"backup_dir,omitempty"`
	Verify         release.VerifyReport `json:"verify"`
	Message        string               `json:"message,omitempty"`
	AppliedAt      time.Time            `json:"applied_at,omitempty"`
}

// RollbackResult describes a local update rollback attempt.
type RollbackResult struct {
	RolledBack bool      `json:"rolled_back"`
	BackupDir  string    `json:"backup_dir,omitempty"`
	InstallDir string    `json:"install_dir,omitempty"`
	RolledAt   time.Time `json:"rolled_at,omitempty"`
}

// Status is persisted by Manager for the Admin UI.
type Status struct {
	CurrentVersion    string               `json:"current_version"`
	Platform          string               `json:"platform"`
	Repository        string               `json:"repository"`
	InstallDir        string               `json:"install_dir,omitempty"`
	StateDir          string               `json:"state_dir,omitempty"`
	DownloadDir       string               `json:"download_dir,omitempty"`
	LatestVersion     string               `json:"latest_version,omitempty"`
	LatestTag         string               `json:"latest_tag,omitempty"`
	UpdateAvailable   bool                 `json:"update_available"`
	AssetName         string               `json:"asset_name,omitempty"`
	AssetURL          string               `json:"asset_url,omitempty"`
	ReleaseURL        string               `json:"release_url,omitempty"`
	PublishedAt       time.Time            `json:"published_at,omitempty"`
	LastCheckedAt     time.Time            `json:"last_checked_at,omitempty"`
	LastCheckError    string               `json:"last_check_error,omitempty"`
	CachedBundleDir   string               `json:"cached_bundle_dir,omitempty"`
	CachedArchivePath string               `json:"cached_archive_path,omitempty"`
	CachedVersion     string               `json:"cached_version,omitempty"`
	CachedVerify      release.VerifyReport `json:"cached_verify"`
	LastAppliedAt     time.Time            `json:"last_applied_at,omitempty"`
	LastApplyError    string               `json:"last_apply_error,omitempty"`
	LastBackupDir     string               `json:"last_backup_dir,omitempty"`
	LastRolledBackAt  time.Time            `json:"last_rolled_back_at,omitempty"`
	LastRollbackError string               `json:"last_rollback_error,omitempty"`
	Message           string               `json:"message,omitempty"`
}

// Manager coordinates online release checks, bundle cache, apply, and rollback.
type Manager struct {
	opts Options
}

// NewManager creates an update manager with normalized defaults.
func NewManager(opts Options) *Manager {
	return &Manager{opts: normalizeOptions(opts)}
}

// Status returns the persisted status plus current runtime defaults.
func (m *Manager) Status() (*Status, error) {
	opts := normalizeOptions(m.opts)
	status := baseStatus(opts)
	if stored, err := loadStatus(opts.StateDir); err == nil && stored != nil {
		mergeStatus(status, stored)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return status, nil
}

// Check queries the latest GitHub release and persists the result.
func (m *Manager) Check(ctx context.Context) (*Status, error) {
	opts := normalizeOptions(m.opts)
	status, _ := m.Status()
	if status == nil {
		status = baseStatus(opts)
	}
	result, err := CheckLatest(ctx, opts)
	if err != nil {
		status.LastCheckedAt = time.Now().UTC()
		status.LastCheckError = err.Error()
		status.Message = "update check failed"
		_ = saveStatus(opts.StateDir, status)
		return status, err
	}
	applyCheck(status, result)
	status.LastCheckError = ""
	status.Message = result.Message
	if err := saveStatus(opts.StateDir, status); err != nil {
		return status, err
	}
	return status, nil
}

// Fetch downloads and verifies the matching latest release archive.
func (m *Manager) Fetch(ctx context.Context, force bool) (*Status, error) {
	opts := normalizeOptions(m.opts)
	status, _ := m.Status()
	if status == nil {
		status = baseStatus(opts)
	}
	result, err := FetchLatest(ctx, opts, force)
	if err != nil {
		status.LastCheckError = err.Error()
		status.Message = "update download failed"
		_ = saveStatus(opts.StateDir, status)
		return status, err
	}
	applyCheck(status, result.CheckResult)
	status.CachedBundleDir = result.BundleDir
	status.CachedArchivePath = result.ArchivePath
	status.CachedVersion = result.Manifest.ProductVersion
	status.CachedVerify = result.Verify
	status.LastCheckError = ""
	status.Message = "update bundle downloaded and verified"
	if err := saveStatus(opts.StateDir, status); err != nil {
		return status, err
	}
	return status, nil
}

// Apply applies a cached, manual, or freshly downloaded update bundle.
func (m *Manager) Apply(ctx context.Context, bundleDir string, download bool, dryRun bool, force bool) (*Status, error) {
	opts := normalizeOptions(m.opts)
	status, _ := m.Status()
	if status == nil {
		status = baseStatus(opts)
	}
	bundleDir = strings.TrimSpace(bundleDir)
	if bundleDir == "" && download {
		fetched, err := FetchLatest(ctx, opts, force)
		if err != nil {
			status.LastApplyError = err.Error()
			status.Message = "update download failed"
			_ = saveStatus(opts.StateDir, status)
			return status, err
		}
		applyCheck(status, fetched.CheckResult)
		status.CachedBundleDir = fetched.BundleDir
		status.CachedArchivePath = fetched.ArchivePath
		status.CachedVersion = fetched.Manifest.ProductVersion
		status.CachedVerify = fetched.Verify
		bundleDir = fetched.BundleDir
	}
	if bundleDir == "" {
		bundleDir = strings.TrimSpace(status.CachedBundleDir)
	}
	if bundleDir == "" {
		err := fmt.Errorf("no update bundle is available; check and download an update first")
		status.LastApplyError = err.Error()
		status.Message = "no verified bundle available"
		_ = saveStatus(opts.StateDir, status)
		return status, err
	}

	result, err := ApplyBundle(ApplyOptions{
		BundleRoot: bundleDir,
		InstallDir: opts.InstallDir,
		StateDir:   opts.StateDir,
		DryRun:     dryRun,
	})
	status.CachedBundleDir = bundleDir
	status.CachedVersion = result.ProductVersion
	status.CachedVerify = result.Verify
	if err != nil {
		status.LastApplyError = err.Error()
		status.Message = "update apply failed"
		_ = saveStatus(opts.StateDir, status)
		return status, err
	}
	status.LastApplyError = ""
	status.LastAppliedAt = result.AppliedAt
	status.LastBackupDir = result.BackupDir
	status.Message = result.Message
	if err := saveStatus(opts.StateDir, status); err != nil {
		return status, err
	}
	return status, nil
}

// Rollback restores the most recent payload backup.
func (m *Manager) Rollback() (*Status, error) {
	opts := normalizeOptions(m.opts)
	status, _ := m.Status()
	if status == nil {
		status = baseStatus(opts)
	}
	result, err := Rollback(RollbackOptions{InstallDir: opts.InstallDir, StateDir: opts.StateDir})
	if err != nil {
		status.LastRollbackError = err.Error()
		status.Message = "update rollback failed"
		_ = saveStatus(opts.StateDir, status)
		return status, err
	}
	status.LastRollbackError = ""
	status.LastRolledBackAt = result.RolledAt
	status.LastBackupDir = result.BackupDir
	status.Message = "rollback restored the last update backup"
	if err := saveStatus(opts.StateDir, status); err != nil {
		return status, err
	}
	return status, nil
}

// CheckLatest queries the latest release for the configured repository.
func CheckLatest(ctx context.Context, opts Options) (CheckResult, error) {
	opts = normalizeOptions(opts)
	releaseInfo, err := fetchLatestRelease(ctx, opts)
	if err != nil {
		return CheckResult{}, err
	}
	latestVersion := NormalizeVersion(releaseInfo.TagName)
	asset := findReleaseAsset(releaseInfo.Assets, ArchiveNameForPlatform(opts.Platform))
	result := CheckResult{
		CurrentVersion:  opts.CurrentVersion,
		LatestVersion:   latestVersion,
		LatestTag:       releaseInfo.TagName,
		UpdateAvailable: CompareVersions(opts.CurrentVersion, latestVersion) < 0,
		Platform:        opts.Platform,
		Repository:      opts.Repository,
		ReleaseURL:      releaseInfo.HTMLURL,
		PublishedAt:     releaseInfo.PublishedAt,
		Prerelease:      releaseInfo.Prerelease,
		CheckedAt:       time.Now().UTC(),
	}
	if asset != nil {
		result.AssetName = asset.Name
		result.AssetURL = asset.BrowserDownloadURL
		result.AssetSize = asset.Size
	}
	if result.UpdateAvailable {
		result.Message = "new version available"
	} else if !IsReleaseVersion(opts.CurrentVersion) {
		result.Message = "current build is not a release version"
	} else {
		result.Message = "already on the latest release"
	}
	if asset == nil {
		result.Message = "latest release has no asset for this platform"
	}
	return result, nil
}

// FetchLatest downloads and verifies the latest platform archive.
func FetchLatest(ctx context.Context, opts Options, force bool) (FetchResult, error) {
	check, err := CheckLatest(ctx, opts)
	if err != nil {
		return FetchResult{}, err
	}
	if check.AssetURL == "" {
		return FetchResult{CheckResult: check}, fmt.Errorf("release %s has no %s asset", check.LatestTag, check.Platform)
	}
	if !force && !check.UpdateAvailable {
		return FetchResult{CheckResult: check}, fmt.Errorf("no newer release available")
	}

	opts = normalizeOptions(opts)
	targetDir := filepath.Join(opts.DownloadDir, safeFileName(check.LatestTag+"-"+check.Platform))
	if err := os.RemoveAll(targetDir); err != nil {
		return FetchResult{CheckResult: check}, err
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return FetchResult{CheckResult: check}, err
	}

	archivePath := filepath.Join(targetDir, check.AssetName)
	if err := downloadFile(ctx, opts.HTTPClient, check.AssetURL, archivePath); err != nil {
		return FetchResult{CheckResult: check, ArchivePath: archivePath}, err
	}
	bundleDir := filepath.Join(targetDir, "bundle")
	if err := extractArchive(archivePath, bundleDir); err != nil {
		return FetchResult{CheckResult: check, ArchivePath: archivePath, BundleDir: bundleDir}, err
	}

	manifest, err := release.LoadManifest(filepath.Join(bundleDir, release.ManifestFileName))
	if err != nil {
		return FetchResult{CheckResult: check, ArchivePath: archivePath, BundleDir: bundleDir}, err
	}
	verify := release.VerifyIncomingBundle(bundleDir, manifest)
	result := FetchResult{
		CheckResult: check,
		ArchivePath: archivePath,
		BundleDir:   bundleDir,
		Manifest:    manifest,
		Verify:      verify,
		Downloaded:  true,
	}
	if !verify.OK {
		return result, fmt.Errorf("downloaded bundle verification failed: %s", strings.Join(verify.Issues, "; "))
	}
	return result, nil
}

// ApplyBundle verifies and copies an incoming bundle into the install directory.
func ApplyBundle(opts ApplyOptions) (ApplyResult, error) {
	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		return ApplyResult{}, fmt.Errorf("bundle root is required")
	}
	installDir := strings.TrimSpace(opts.InstallDir)
	if installDir == "" {
		installDir = "."
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(".gateway-runtime", "update")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	manifest, err := release.LoadManifest(filepath.Join(bundleRoot, release.ManifestFileName))
	if err != nil {
		return ApplyResult{BundleRoot: bundleRoot, InstallDir: installDir}, err
	}
	verify := release.VerifyIncomingBundle(bundleRoot, manifest)
	result := ApplyResult{
		DryRun:         opts.DryRun,
		ProductVersion: manifest.ProductVersion,
		BundleRoot:     bundleRoot,
		InstallDir:     installDir,
		Verify:         verify,
	}
	if !verify.OK {
		return result, fmt.Errorf("bundle verification failed: %s", strings.Join(verify.Issues, "; "))
	}
	backupDir := filepath.Join(stateDir, "backups", now.Format("20060102-150405"))
	result.BackupDir = backupDir
	if opts.DryRun {
		result.Message = "dry-run: bundle verified; no files copied"
		return result, nil
	}
	if err := backupInstallPayload(installDir, backupDir, manifest); err != nil {
		return result, err
	}
	if err := copyBundlePayload(bundleRoot, installDir, manifest); err != nil {
		_ = restoreInstallPayload(backupDir, installDir)
		return result, fmt.Errorf("copy bundle payload: %w", err)
	}
	result.Applied = true
	result.AppliedAt = now
	result.Message = "update applied; restart the service to run the new binaries"
	return result, nil
}

// RollbackOptions controls payload rollback.
type RollbackOptions struct {
	InstallDir string
	StateDir   string
}

// Rollback restores the most recent update backup.
func Rollback(opts RollbackOptions) (RollbackResult, error) {
	installDir := strings.TrimSpace(opts.InstallDir)
	if installDir == "" {
		installDir = "."
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(".gateway-runtime", "update")
	}
	backupDir, err := latestBackup(filepath.Join(stateDir, "backups"))
	if err != nil {
		return RollbackResult{InstallDir: installDir}, err
	}
	if err := restoreInstallPayload(backupDir, installDir); err != nil {
		return RollbackResult{InstallDir: installDir, BackupDir: backupDir}, err
	}
	return RollbackResult{RolledBack: true, BackupDir: backupDir, InstallDir: installDir, RolledAt: time.Now().UTC()}, nil
}

// ArchiveNameForPlatform maps GOOS/GOARCH to the release archive name.
func ArchiveNameForPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	parts := strings.Split(platform, "/")
	if len(parts) != 2 {
		return ""
	}
	goos, goarch := parts[0], parts[1]
	if goos == "windows" {
		return fmt.Sprintf("ai-model-gateway-%s-%s.zip", goos, goarch)
	}
	return fmt.Sprintf("ai-model-gateway-%s-%s.tar.gz", goos, goarch)
}

// NormalizeVersion strips a leading v and build metadata.
func NormalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if idx := strings.Index(value, "+"); idx >= 0 {
		value = value[:idx]
	}
	return value
}

// IsReleaseVersion reports whether value looks like a SemVer release.
func IsReleaseVersion(value string) bool {
	parsed, ok := parseVersion(value)
	return ok && len(parsed.nums) == 3
}

// CompareVersions compares SemVer-ish versions. It returns -1, 0, or 1.
func CompareVersions(left string, right string) int {
	a, okA := parseVersion(left)
	b, okB := parseVersion(right)
	if !okA && !okB {
		return strings.Compare(NormalizeVersion(left), NormalizeVersion(right))
	}
	if !okA {
		return 0
	}
	if !okB {
		return 0
	}
	for i := 0; i < 3; i++ {
		if a.nums[i] < b.nums[i] {
			return -1
		}
		if a.nums[i] > b.nums[i] {
			return 1
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}

type parsedVersion struct {
	nums [3]int
	pre  string
}

func parseVersion(value string) (parsedVersion, bool) {
	value = NormalizeVersion(value)
	if value == "" {
		return parsedVersion{}, false
	}
	base := value
	pre := ""
	if idx := strings.Index(base, "-"); idx >= 0 {
		pre = base[idx+1:]
		base = base[:idx]
	}
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return parsedVersion{}, false
	}
	var parsed parsedVersion
	parsed.pre = pre
	for i, part := range parts {
		if part == "" {
			return parsedVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsedVersion{}, false
		}
		parsed.nums[i] = n
	}
	return parsed, true
}

func fetchLatestRelease(ctx context.Context, opts Options) (ReleaseInfo, error) {
	repository := strings.Trim(strings.TrimSpace(opts.Repository), "/")
	if repository == "" || !strings.Contains(repository, "/") {
		return ReleaseInfo{}, fmt.Errorf("repository must be owner/name")
	}
	apiBase := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if apiBase == "" {
		apiBase = DefaultAPIBaseURL
	}
	rawURL := apiBase + "/repos/" + repository + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AI-Model-Gateway-Updater")
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ReleaseInfo{}, fmt.Errorf("latest release query failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var releaseInfo ReleaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&releaseInfo); err != nil {
		return ReleaseInfo{}, fmt.Errorf("decode latest release: %w", err)
	}
	if strings.TrimSpace(releaseInfo.TagName) == "" {
		return ReleaseInfo{}, fmt.Errorf("latest release response missing tag_name")
	}
	return releaseInfo, nil
}

func findReleaseAsset(assets []Asset, want string) *Asset {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return nil
	}
	for i := range assets {
		if strings.ToLower(strings.TrimSpace(assets[i].Name)) == want {
			return &assets[i]
		}
	}
	return nil
}

func downloadFile(ctx context.Context, client *http.Client, rawURL string, dst string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("download URL is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AI-Model-Gateway-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func extractArchive(archivePath string, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, dst)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, dst)
	default:
		return fmt.Errorf("unsupported archive type: %s", filepath.Base(archivePath))
	}
}

func extractZip(archivePath string, dst string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target, err := safeArchivePath(dst, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		_ = in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func extractTarGz(archivePath string, dst string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeArchivePath(dst, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode).Perm()
			if mode == 0 {
				mode = 0644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, reader)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func safeArchivePath(root string, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("archive path escapes destination: %q", name)
	}
	return target, nil
}

func copyPathIfExists(src string, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return copyPath(src, dst)
}

func copyPath(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			return copyFile(path, target)
		})
	}
	return copyFile(src, dst)
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func backupInstallPayload(installDir string, backupDir string, manifest release.Manifest) error {
	paths := []string{release.ManifestFileName}
	for _, binary := range manifest.Binaries {
		paths = append(paths, binary.Path)
	}
	if strings.TrimSpace(manifest.AdminDistHash) != "" {
		paths = append(paths, filepath.ToSlash(filepath.Join("web", "admin", "dist")))
	}
	for _, rel := range paths {
		if err := copyPathIfExists(filepath.Join(installDir, filepath.FromSlash(rel)), filepath.Join(backupDir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

func copyBundlePayload(bundleRoot string, installDir string, manifest release.Manifest) error {
	if err := copyFile(filepath.Join(bundleRoot, release.ManifestFileName), filepath.Join(installDir, release.ManifestFileName)); err != nil {
		return err
	}
	for _, binary := range manifest.Binaries {
		src := filepath.Join(bundleRoot, filepath.FromSlash(binary.Path))
		dst := filepath.Join(installDir, filepath.FromSlash(binary.Path))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	if strings.TrimSpace(manifest.AdminDistHash) != "" {
		src := filepath.Join(bundleRoot, "web", "admin", "dist")
		dst := filepath.Join(installDir, "web", "admin", "dist")
		if err := replaceDir(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func replaceDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyPath(src, tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	cleanupTmp = false
	return nil
}

func restoreInstallPayload(backupDir string, installDir string) error {
	return filepath.WalkDir(backupDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(backupDir, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(installDir, rel))
	})
}

func latestBackup(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("no update backups found in %s", root)
	}
	return filepath.Join(root, names[len(names)-1]), nil
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.CurrentVersion) == "" {
		opts.CurrentVersion = version.ProductVersion
	}
	if strings.TrimSpace(opts.Repository) == "" {
		opts.Repository = DefaultRepository
	}
	if strings.TrimSpace(opts.APIBaseURL) == "" {
		opts.APIBaseURL = DefaultAPIBaseURL
	}
	if strings.TrimSpace(opts.Platform) == "" {
		opts.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		opts.StateDir = filepath.Join(".gateway-runtime", "update")
	}
	if strings.TrimSpace(opts.DownloadDir) == "" {
		opts.DownloadDir = filepath.Join(opts.StateDir, "downloads")
	}
	if strings.TrimSpace(opts.InstallDir) == "" {
		opts.InstallDir = "."
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return opts
}

func baseStatus(opts Options) *Status {
	return &Status{
		CurrentVersion: opts.CurrentVersion,
		Platform:       opts.Platform,
		Repository:     opts.Repository,
		InstallDir:     opts.InstallDir,
		StateDir:       opts.StateDir,
		DownloadDir:    opts.DownloadDir,
	}
}

func mergeStatus(base *Status, stored *Status) {
	currentVersion := base.CurrentVersion
	platform := base.Platform
	repository := base.Repository
	installDir := base.InstallDir
	stateDir := base.StateDir
	downloadDir := base.DownloadDir
	*base = *stored
	base.CurrentVersion = currentVersion
	base.Platform = platform
	base.Repository = repository
	base.InstallDir = installDir
	base.StateDir = stateDir
	base.DownloadDir = downloadDir
}

func applyCheck(status *Status, result CheckResult) {
	status.CurrentVersion = result.CurrentVersion
	status.Platform = result.Platform
	status.Repository = result.Repository
	status.LatestVersion = result.LatestVersion
	status.LatestTag = result.LatestTag
	status.UpdateAvailable = result.UpdateAvailable
	status.AssetName = result.AssetName
	status.AssetURL = result.AssetURL
	status.ReleaseURL = result.ReleaseURL
	status.PublishedAt = result.PublishedAt
	status.LastCheckedAt = result.CheckedAt
}

func loadStatus(stateDir string) (*Status, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, stateFileName))
	if err != nil {
		return nil, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func saveStatus(stateDir string, status *Status) error {
	if status == nil {
		return nil
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(stateDir, stateFileName), data, 0644)
}

func safeFileName(value string) string {
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.TrimSpace(value)
	if value == "" {
		return "latest"
	}
	return value
}
