package updater

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ai-model-gateway/internal/release"
)

func writeBundleBinary(t *testing.T, binDir, name string, contents []byte) {
	t.Helper()
	path := filepath.Join(binDir, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func createBundle(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aigw", "gatewayd", "controld", "telemetryd", "gateway-cli"} {
		writeBundleBinary(t, binDir, name, []byte("binary:"+name+":"+version))
	}
	adminDist := filepath.Join(root, "web", "admin", "dist")
	if err := os.MkdirAll(adminDist, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminDist, "index.html"), []byte(version), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := release.BuildManifest(release.BuildOptions{Root: root, ProductVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	if err := release.SaveManifest(filepath.Join(root, release.ManifestFileName), manifest); err != nil {
		t.Fatal(err)
	}
	return root
}

func zipBundle(t *testing.T, bundleRoot string, archivePath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	if err := filepath.WalkDir(bundleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(bundleRoot, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(bundleRoot, rel))
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.4.1", "1.4.2") >= 0 {
		t.Fatal("1.4.1 should be older than 1.4.2")
	}
	if CompareVersions("v1.4.2", "1.4.2") != 0 {
		t.Fatal("v prefix should not affect equality")
	}
	if CompareVersions("1.4.2", "1.4.2-rc.1") <= 0 {
		t.Fatal("release should sort after prerelease")
	}
}

func TestArchiveNameForPlatform(t *testing.T) {
	if got := ArchiveNameForPlatform("windows/amd64"); got != "ai-model-gateway-windows-amd64.zip" {
		t.Fatalf("windows archive = %q", got)
	}
	if got := ArchiveNameForPlatform("linux/arm64"); got != "ai-model-gateway-linux-arm64.tar.gz" {
		t.Fatalf("linux archive = %q", got)
	}
}

func TestCheckLatestFindsMatchingAsset(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(ReleaseInfo{
			TagName: "v1.4.2",
			HTMLURL: "https://example.test/release",
			Assets: []Asset{
				{Name: "ai-model-gateway-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/linux.tar.gz", Size: 12},
			},
		})
	}))
	defer api.Close()

	result, err := CheckLatest(context.Background(), Options{
		CurrentVersion: "1.4.1",
		Repository:     "owner/repo",
		APIBaseURL:     api.URL,
		Platform:       "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable {
		t.Fatal("UpdateAvailable = false, want true")
	}
	if result.AssetName != "ai-model-gateway-linux-amd64.tar.gz" {
		t.Fatalf("AssetName = %q", result.AssetName)
	}
}

func TestFetchLatestDownloadsAndVerifiesZip(t *testing.T) {
	bundle := createBundle(t, "1.4.2")
	archive := filepath.Join(t.TempDir(), "ai-model-gateway-windows-amd64.zip")
	zipBundle(t, bundle, archive)

	var serverURL string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/releases/latest":
			_ = json.NewEncoder(w).Encode(ReleaseInfo{
				TagName: "v1.4.2",
				Assets: []Asset{{
					Name:               "ai-model-gateway-windows-amd64.zip",
					BrowserDownloadURL: serverURL + "/asset.zip",
					Size:               123,
				}},
			})
		case "/asset.zip":
			http.ServeFile(w, r, archive)
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = api.URL
	defer api.Close()

	result, err := FetchLatest(context.Background(), Options{
		CurrentVersion: "1.4.1",
		Repository:     "owner/repo",
		APIBaseURL:     api.URL,
		Platform:       "windows/amd64",
		DownloadDir:    t.TempDir(),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verify.OK {
		t.Fatalf("verify issues = %#v", result.Verify.Issues)
	}
	if result.Manifest.ProductVersion != "1.4.2" {
		t.Fatalf("manifest version = %q", result.Manifest.ProductVersion)
	}
}

func TestExtractArchiveRejectsUnsafeZipPath(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bad.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	err = extractArchive(archive, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("extractArchive error = %v, want unsafe path", err)
	}
}

func TestSafeArchivePathRejectsTraversal(t *testing.T) {
	if _, err := safeArchivePath(t.TempDir(), "../../evil.txt"); err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("safeArchivePath error = %v, want unsafe path", err)
	}
}

func TestApplyBundleCopiesPayloadAndRollbackRestoresBackup(t *testing.T) {
	bundle := createBundle(t, "1.4.2")
	install := createBundle(t, "1.4.1")
	state := t.TempDir()

	result, err := ApplyBundle(ApplyOptions{BundleRoot: bundle, InstallDir: install, StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.BackupDir == "" {
		t.Fatalf("apply result = %#v", result)
	}
	manifest, err := release.LoadManifest(filepath.Join(install, release.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProductVersion != "1.4.2" {
		t.Fatalf("installed version = %q", manifest.ProductVersion)
	}
	rollback, err := Rollback(RollbackOptions{InstallDir: install, StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if !rollback.RolledBack {
		t.Fatalf("rollback = %#v", rollback)
	}
	manifest, err = release.LoadManifest(filepath.Join(install, release.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProductVersion != "1.4.1" {
		t.Fatalf("rolled back version = %q", manifest.ProductVersion)
	}
}
