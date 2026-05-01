package release

import (
	"os"
	"path/filepath"
	"runtime"
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
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
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
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(name), 0755); err != nil {
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
