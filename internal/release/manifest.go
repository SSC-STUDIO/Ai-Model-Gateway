// Package release builds and verifies versioned AI Model Gateway bundles.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/version"
)

const (
	// ManifestFileName is the default manifest name at a bundle root.
	ManifestFileName = "aigw-manifest.json"

	manifestSchemaVersion = 1
)

var requiredDaemons = []string{"telemetryd", "gatewayd", "controld"}
var shippedBinaries = []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"}

// requiredForUpdate lists binaries that must be present in an incoming update bundle.
var requiredForUpdate = []string{"aigw", "telemetryd", "gatewayd", "controld"}

// Manifest describes a single atomic release payload.
type Manifest struct {
	SchemaVersion         int                       `json:"schema_version"`
	ProductVersion        string                    `json:"product_version"`
	GitCommit             string                    `json:"git_commit,omitempty"`
	BuiltAt               string                    `json:"built_at"`
	Platform              string                    `json:"platform"`
	Binaries              map[string]BinaryManifest `json:"binaries"`
	AdminDistHash         string                    `json:"admin_dist_hash,omitempty"`
	SnapshotSchemaVersion int                       `json:"snapshot_schema_version"`
	RPCContractVersion    string                    `json:"rpc_contract_version"`
	RequiredDaemons       []string                  `json:"required_daemons"`
	DefaultConfigPaths    map[string]string         `json:"default_config_paths"`
	Migration             MigrationManifest         `json:"migration"`
}

// BinaryManifest records one executable in a release payload.
type BinaryManifest struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// MigrationManifest describes upgrade compatibility requirements.
type MigrationManifest struct {
	MinVersion string `json:"min_version,omitempty"`
	Required   bool   `json:"required"`
	Notes      string `json:"notes,omitempty"`
}

// BuildOptions controls manifest generation.
type BuildOptions struct {
	Root           string
	ProductVersion string
	GitCommit      string
	BuiltAt        time.Time
	Platform       string
}

// VerifyReport describes manifest verification results.
type VerifyReport struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues,omitempty"`
}

type verifyOptions struct {
	requireCurrentProductVersion bool
	requireCurrentRPCContract    bool
	requireCurrentSnapshotSchema bool
	requireShippedBinaries       bool
	requireUpdateBinaries        bool
}

// BuildManifest creates a manifest by hashing known bundle files under Root.
func BuildManifest(opts BuildOptions) (Manifest, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	productVersion := strings.TrimSpace(opts.ProductVersion)
	if productVersion == "" {
		productVersion = version.ProductVersion
	}
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	builtAt := opts.BuiltAt
	if builtAt.IsZero() {
		builtAt = time.Now().UTC()
	}

	manifest := Manifest{
		SchemaVersion:         manifestSchemaVersion,
		ProductVersion:        productVersion,
		GitCommit:             strings.TrimSpace(opts.GitCommit),
		BuiltAt:               builtAt.UTC().Format(time.RFC3339),
		Platform:              platform,
		Binaries:              make(map[string]BinaryManifest),
		SnapshotSchemaVersion: snapshot.CurrentSchemaVersion,
		RPCContractVersion:    version.RPCContractVersion,
		RequiredDaemons:       append([]string(nil), requiredDaemons...),
		DefaultConfigPaths: map[string]string{
			"gatewayd":   "configs/gatewayd.json",
			"controld":   "configs/controld.json",
			"telemetryd": "configs/telemetryd.json",
		},
		Migration: MigrationManifest{
			MinVersion: "1.2.0",
			Required:   false,
			Notes:      "v1.3 bundles must be applied as one aigw-managed payload.",
		},
	}

	for _, name := range shippedBinaries {
		relPath, path, err := findBundleBinary(root, name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Manifest{}, err
		}
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Manifest{}, fmt.Errorf("stat %s: %w", relPath, err)
		}
		sum, err := HashFile(path)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Binaries[name] = BinaryManifest{
			Path:    relPath,
			SHA256:  sum,
			Version: productVersion,
		}
	}

	adminDir := filepath.Join(root, "web", "admin", "dist")
	if _, err := os.Stat(adminDir); err == nil {
		sum, err := HashDir(adminDir)
		if err != nil {
			return Manifest{}, err
		}
		manifest.AdminDistHash = sum
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("stat admin dist: %w", err)
	}

	return manifest, nil
}

// SaveManifest writes manifest as stable indented JSON.
func SaveManifest(path string, manifest Manifest) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("manifest path is required")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadManifest reads a manifest from disk.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

// VerifyManifest verifies manifest hashes and compatibility for Root.
func VerifyManifest(root string, manifest Manifest) VerifyReport {
	return verifyManifest(root, manifest, verifyOptions{
		requireCurrentProductVersion: true,
		requireCurrentRPCContract:    true,
		requireCurrentSnapshotSchema: true,
		requireShippedBinaries:       true,
	})
}

// VerifyIncomingBundle verifies a bundle payload before it replaces the
// currently installed binaries. It checks payload integrity and manifest
// consistency without requiring the running aigw binary to have the same
// product, RPC, or snapshot-schema version as the incoming bundle.
// Unlike VerifyManifest, this also requires the aigw supervisor binary to be
// present, since an update that omits aigw would leave the supervisor mismatched
// with the daemon versions on next restart.
func VerifyIncomingBundle(root string, manifest Manifest) VerifyReport {
	return verifyManifest(root, manifest, verifyOptions{
		requireShippedBinaries: true,
		requireUpdateBinaries:  true,
	})
}

func verifyManifest(root string, manifest Manifest, opts verifyOptions) VerifyReport {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)
	report := VerifyReport{OK: true}
	addIssue := func(format string, args ...any) {
		report.OK = false
		report.Issues = append(report.Issues, fmt.Sprintf(format, args...))
	}

	if manifest.SchemaVersion != manifestSchemaVersion {
		addIssue("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.ProductVersion) == "" {
		addIssue("product_version is required")
	}
	if opts.requireCurrentProductVersion && manifest.ProductVersion != "" && manifest.ProductVersion != version.ProductVersion {
		addIssue("manifest product_version %s does not match binary product_version %s", manifest.ProductVersion, version.ProductVersion)
	}
	if opts.requireCurrentRPCContract && manifest.RPCContractVersion != "" && manifest.RPCContractVersion != version.RPCContractVersion {
		addIssue("manifest rpc_contract_version %s does not match binary rpc_contract_version %s", manifest.RPCContractVersion, version.RPCContractVersion)
	}
	if opts.requireCurrentSnapshotSchema && manifest.SnapshotSchemaVersion != 0 && manifest.SnapshotSchemaVersion != snapshot.CurrentSchemaVersion {
		addIssue("manifest snapshot_schema_version %d does not match binary snapshot_schema_version %d", manifest.SnapshotSchemaVersion, snapshot.CurrentSchemaVersion)
	}

	currentPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if manifest.Platform != "" && manifest.Platform != currentPlatform {
		addIssue("manifest platform %s does not match current platform %s", manifest.Platform, currentPlatform)
	}

	for _, daemon := range requiredDaemons {
		binary, ok := manifest.Binaries[daemon]
		if !ok {
			addIssue("required daemon %s missing from manifest", daemon)
			continue
		}
		if binary.Version != "" && binary.Version != manifest.ProductVersion {
			addIssue("daemon %s version %s does not match manifest product_version %s", daemon, binary.Version, manifest.ProductVersion)
		}
		verifyBinary(root, daemon, binary, addIssue)
	}
	if opts.requireUpdateBinaries {
		for _, name := range requiredForUpdate {
			if _, ok := manifest.Binaries[name]; !ok {
				addIssue("required binary %s missing from manifest", name)
			}
		}
	}
	if opts.requireShippedBinaries {
		for _, name := range shippedBinaries {
			if _, ok := manifest.Binaries[name]; !ok {
				addIssue("shipped binary %s missing from manifest", name)
			}
		}
	}
	for name, binary := range manifest.Binaries {
		if contains(requiredDaemons, name) {
			continue
		}
		verifyBinary(root, name, binary, addIssue)
	}

	if manifest.AdminDistHash != "" {
		adminDir := filepath.Join(root, "web", "admin", "dist")
		sum, err := HashDir(adminDir)
		if err != nil {
			addIssue("admin dist hash failed: %v", err)
		} else if !strings.EqualFold(sum, manifest.AdminDistHash) {
			addIssue("admin dist hash mismatch: got %s want %s", sum, manifest.AdminDistHash)
		}
	}

	sort.Strings(report.Issues)
	return report
}

// HashFile returns the SHA-256 hex digest of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashDir returns a deterministic hash of a directory tree.
func HashDir(root string) (string, error) {
	root = filepath.Clean(root)
	entries := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(entries)

	h := sha256.New()
	for _, rel := range entries {
		path := filepath.Join(root, filepath.FromSlash(rel))
		fileHash, err := HashFile(path)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(h, rel)
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, fileHash)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyBinary(root string, name string, binary BinaryManifest, addIssue func(string, ...any)) {
	if strings.TrimSpace(binary.Path) == "" {
		addIssue("binary %s path is required", name)
		return
	}
	path := filepath.Join(root, filepath.FromSlash(binary.Path))
	sum, err := HashFile(path)
	if err != nil {
		addIssue("binary %s hash failed: %v", name, err)
		return
	}
	if !strings.EqualFold(sum, binary.SHA256) {
		addIssue("binary %s hash mismatch: got %s want %s", name, sum, binary.SHA256)
	}
}

func executableRelPathIn(dir string, name string) string {
	if runtime.GOOS == "windows" {
		return dir + "/" + name + ".exe"
	}
	return dir + "/" + name
}

func findBundleBinary(root string, name string) (string, string, error) {
	for _, dir := range []string{"bin", "dist"} {
		relPath := executableRelPathIn(dir, name)
		path := filepath.Join(root, filepath.FromSlash(relPath))
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return relPath, path, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("stat %s: %w", relPath, err)
		}
	}
	return "", "", os.ErrNotExist
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
