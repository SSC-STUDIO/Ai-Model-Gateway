package release

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ai-model-gateway/internal/version"
)

func TestBuildAndVerifyManifest(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"} {
		path := filepath.Join(binDir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := os.WriteFile(path, []byte("binary:"+name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	adminDist := filepath.Join(root, "web", "admin", "dist")
	if err := os.MkdirAll(adminDist, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminDist, "index.html"), []byte("<div></div>"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildManifest(BuildOptions{Root: root, GitCommit: "abc"})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	if manifest.ProductVersion != version.ProductVersion {
		t.Fatalf("ProductVersion = %q, want %q", manifest.ProductVersion, version.ProductVersion)
	}
	if len(manifest.Binaries) != 5 {
		t.Fatalf("len(Binaries) = %d, want 5", len(manifest.Binaries))
	}
	if manifest.AdminDistHash == "" {
		t.Fatal("AdminDistHash is empty")
	}

	report := VerifyManifest(root, manifest)
	if !report.OK {
		t.Fatalf("VerifyManifest() issues = %#v", report.Issues)
	}
}

func TestVerifyManifestRejectsMixedDaemonVersion(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gatewayd", "controld", "telemetryd"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	binary := manifest.Binaries["gatewayd"]
	binary.Version = "9.9.9"
	manifest.Binaries["gatewayd"] = binary

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false")
	}
	found := false
	for _, issue := range report.Issues {
		if issue == "daemon gatewayd version 9.9.9 does not match manifest product_version "+manifest.ProductVersion {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want mixed daemon version issue", report.Issues)
	}
}

func TestVerifyIncomingBundleAllowsNewerBundleIdentity(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Include all required binaries for an update bundle (including aigw).
	for _, name := range []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"} {
		path := filepath.Join(binDir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root, ProductVersion: "9.9.9"})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	manifest.RPCContractVersion = "99"
	manifest.SnapshotSchemaVersion = 99

	localReport := VerifyManifest(root, manifest)
	if localReport.OK {
		t.Fatal("VerifyManifest() OK = true, want false for newer bundle identity")
	}
	incomingReport := VerifyIncomingBundle(root, manifest)
	if !incomingReport.OK {
		t.Fatalf("VerifyIncomingBundle() issues = %#v", incomingReport.Issues)
	}
}

func TestVerifyIncomingBundleRejectsMissingAigw(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Only include daemon binaries, omit aigw.
	for _, name := range []string{"gatewayd", "controld", "telemetryd"} {
		path := filepath.Join(binDir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root, ProductVersion: "9.9.9"})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	report := VerifyIncomingBundle(root, manifest)
	if report.OK {
		t.Fatal("VerifyIncomingBundle() OK = true, want false for missing aigw")
	}
	found := false
	for _, issue := range report.Issues {
		if issue == "required binary aigw missing from manifest" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want missing aigw issue", report.Issues)
	}
}

func TestSaveAndLoadManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "manifest.json")

	original := Manifest{
		SchemaVersion:  manifestSchemaVersion,
		ProductVersion: "1.0.0",
		GitCommit:      "deadbeef",
		BuiltAt:        "2026-01-01T00:00:00Z",
		Platform:       "linux/amd64",
		Binaries: map[string]BinaryManifest{
			"gatewayd": {Path: "bin/gatewayd", SHA256: "abc123", Version: "1.0.0"},
		},
		RequiredDaemons:    []string{"gatewayd", "controld", "telemetryd"},
		DefaultConfigPaths: map[string]string{"gatewayd": "configs/gatewayd.json"},
	}

	if err := SaveManifest(path, original); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}

	if loaded.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", loaded.SchemaVersion, original.SchemaVersion)
	}
	if loaded.ProductVersion != original.ProductVersion {
		t.Errorf("ProductVersion = %q, want %q", loaded.ProductVersion, original.ProductVersion)
	}
	if loaded.GitCommit != original.GitCommit {
		t.Errorf("GitCommit = %q, want %q", loaded.GitCommit, original.GitCommit)
	}
	if len(loaded.Binaries) != 1 {
		t.Errorf("len(Binaries) = %d, want 1", len(loaded.Binaries))
	}
	if b, ok := loaded.Binaries["gatewayd"]; !ok || b.SHA256 != "abc123" {
		t.Errorf("Binaries[gatewayd] = %v, want SHA256=abc123", loaded.Binaries["gatewayd"])
	}
}

func TestSaveManifestEmptyPath(t *testing.T) {
	if err := SaveManifest("", Manifest{}); err == nil {
		t.Fatal("SaveManifest('') error = nil, want error")
	}
	if err := SaveManifest("  ", Manifest{}); err == nil {
		t.Fatal("SaveManifest('  ') error = nil, want error")
	}
}

func TestLoadManifestInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("LoadManifest(invalid JSON) error = nil, want error")
	}
}

func TestLoadManifestNonExistent(t *testing.T) {
	if _, err := LoadManifest("/nonexistent/path/manifest.json"); err == nil {
		t.Fatal("LoadManifest(nonexistent) error = nil, want error")
	}
}

func TestBuildManifestDefaultsAndEdgeCases(t *testing.T) {
	t.Run("empty root defaults to dot", func(t *testing.T) {
		// BuildManifest with empty root won't find binaries but should not error
		manifest, err := BuildManifest(BuildOptions{Root: ""})
		if err != nil {
			t.Fatalf("BuildManifest(empty root) error = %v", err)
		}
		if manifest.SchemaVersion != manifestSchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", manifest.SchemaVersion, manifestSchemaVersion)
		}
		if len(manifest.Binaries) != 0 {
			t.Errorf("len(Binaries) = %d, want 0 for empty root", len(manifest.Binaries))
		}
	})

	t.Run("custom product version and platform", func(t *testing.T) {
		manifest, err := BuildManifest(BuildOptions{
			Root:           t.TempDir(),
			ProductVersion: "custom-1.2.3",
			Platform:       "darwin/arm64",
		})
		if err != nil {
			t.Fatalf("BuildManifest() error = %v", err)
		}
		if manifest.ProductVersion != "custom-1.2.3" {
			t.Errorf("ProductVersion = %q, want %q", manifest.ProductVersion, "custom-1.2.3")
		}
		if manifest.Platform != "darwin/arm64" {
			t.Errorf("Platform = %q, want %q", manifest.Platform, "darwin/arm64")
		}
	})

	t.Run("zero builtAt uses current time", func(t *testing.T) {
		manifest, err := BuildManifest(BuildOptions{Root: t.TempDir()})
		if err != nil {
			t.Fatalf("BuildManifest() error = %v", err)
		}
		if manifest.BuiltAt == "" {
			t.Error("BuiltAt is empty, want auto-populated timestamp")
		}
	})

	t.Run("dist directory also searched for binaries", func(t *testing.T) {
		root := t.TempDir()
		distDir := filepath.Join(root, "dist")
		if err := os.MkdirAll(distDir, 0755); err != nil {
			t.Fatal(err)
		}
		name := "aigw"
		path := filepath.Join(distDir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := os.WriteFile(path, []byte("dist-binary"), 0755); err != nil {
			t.Fatal(err)
		}
		manifest, err := BuildManifest(BuildOptions{Root: root})
		if err != nil {
			t.Fatalf("BuildManifest() error = %v", err)
		}
		aigw, ok := manifest.Binaries["aigw"]
		if !ok {
			t.Fatal("aigw binary not found in manifest")
		}
		if aigw.Path != "dist/aigw" && aigw.Path != "dist/aigw.exe" {
			t.Errorf("aigw path = %q, want dist/aigw", aigw.Path)
		}
	})
}

func TestHashFileError(t *testing.T) {
	if _, err := HashFile("/nonexistent/file"); err == nil {
		t.Fatal("HashFile(nonexistent) error = nil, want error")
	}
}

func TestHashDirWithSubdirectories(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "assets", "js")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "app.js"), []byte("console.log()"), 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	if hash == "" {
		t.Fatal("HashDir() returned empty hash")
	}

	// Determinism: same input gives same hash
	hash2, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() second call error = %v", err)
	}
	if hash != hash2 {
		t.Errorf("HashDir() not deterministic: %s != %s", hash, hash2)
	}
}

func TestHashDirError(t *testing.T) {
	if _, err := HashDir("/nonexistent/dir"); err == nil {
		t.Fatal("HashDir(nonexistent) error = nil, want error")
	}
}

func TestVerifyManifestPlatformMismatch(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gatewayd", "controld", "telemetryd"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	manifest.Platform = "fuchsia/riscv64"

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for platform mismatch")
	}
	found := false
	for _, issue := range report.Issues {
		if strings.Contains(issue, "platform") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %#v, want platform mismatch issue", report.Issues)
	}
}

func TestVerifyManifestMissingDaemon(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Only write gatewayd, omit controld and telemetryd
	if err := os.WriteFile(filepath.Join(binDir, "gatewayd"), []byte("g"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildManifest(BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for missing daemons")
	}
	missingCount := 0
	for _, issue := range report.Issues {
		if strings.Contains(issue, "missing from manifest") {
			missingCount++
		}
	}
	if missingCount < 2 {
		t.Errorf("got %d missing-daemon issues, want at least 2; issues = %v", missingCount, report.Issues)
	}
}

func TestVerifyManifestBinaryHashMismatch(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	// Tamper with aigw binary after manifest was built
	aigw := manifest.Binaries["aigw"]
	aigw.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	manifest.Binaries["aigw"] = aigw

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for hash mismatch")
	}
	found := false
	for _, issue := range report.Issues {
		if strings.Contains(issue, "hash mismatch") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %#v, want hash mismatch issue", report.Issues)
	}
}

func TestVerifyManifestEmptySchemaVersion(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{
		SchemaVersion:  99,
		ProductVersion: version.ProductVersion,
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		Binaries:       map[string]BinaryManifest{},
	}

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for bad schema version")
	}
	found := false
	for _, issue := range report.Issues {
		if strings.Contains(issue, "schema version") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %#v, want schema version issue", report.Issues)
	}
}

func TestVerifyManifestEmptyProductVersion(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:  manifestSchemaVersion,
		ProductVersion: "",
		Binaries:       map[string]BinaryManifest{},
	}

	report := VerifyManifest(t.TempDir(), manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for empty product_version")
	}
	found := false
	for _, issue := range report.Issues {
		if strings.Contains(issue, "product_version") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %#v, want product_version issue", report.Issues)
	}
}

func TestVerifyManifestAdminDistHashMismatch(t *testing.T) {
	root := t.TempDir()
	adminDist := filepath.Join(root, "web", "admin", "dist")
	if err := os.MkdirAll(adminDist, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminDist, "index.html"), []byte("<h1>v1</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	manifest, err := BuildManifest(BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	manifest.AdminDistHash = "0000000000000000000000000000000000000000000000000000000000000000"

	report := VerifyManifest(root, manifest)
	if report.OK {
		t.Fatal("VerifyManifest() OK = true, want false for admin dist hash mismatch")
	}
	found := false
	for _, issue := range report.Issues {
		if strings.Contains(issue, "admin dist hash mismatch") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %#v, want admin dist hash mismatch issue", report.Issues)
	}
}
