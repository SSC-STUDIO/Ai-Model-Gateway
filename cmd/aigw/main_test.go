package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/release"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"aigw", "version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(version) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "aigw version "+Version) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestBundleBuildAndVerify(t *testing.T) {
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
	manifestPath := filepath.Join(root, "aigw-manifest.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"aigw", "bundle", "build", "-root", root, "-out", manifestPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bundle build code = %d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"aigw", "bundle", "verify", "-root", root, "-manifest", manifestPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bundle verify code = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "bundle verified") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunStatusUsesAdminToken(t *testing.T) {
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer control.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer gateway.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"aigw", "status", "-control-url", control.URL, "-gateway-url", gateway.URL, "-token", "secret", "-format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(status) code = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode status: %v\n%s", err, stdout.String())
	}
	controlPayload := payload["control"].(map[string]any)
	if controlPayload["ok"] != true {
		t.Fatalf("control payload = %#v", controlPayload)
	}
}

func TestServicePrintUsesStrictManifest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"aigw", "service", "print"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(service print) code = %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Count(out, "ExecStart=") != 1 {
		t.Fatalf("ExecStart count mismatch:\n%s", out)
	}
	for _, want := range []string{"-bin-dir /opt/ai-model-gateway/bin", "-strict-manifest=true", "-manifest /opt/ai-model-gateway/aigw-manifest.json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("service unit missing %q:\n%s", want, out)
		}
	}
}

func TestSuperviseProcessSpecsApplyRuntimeRoot(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "telemetryd.json"), []byte(`{
  "ingest_socket": ".gateway-runtime/telemetry-ingest.sock",
  "query_socket": ".gateway-runtime/telemetry-query.sock",
  "data_dir": ".gateway-runtime/telemetry-migrated"
}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "gatewayd.json"), []byte(`{
  "control_socket": ".gateway-runtime/gateway-control.sock",
  "telemetry_socket": ".gateway-runtime/telemetry-ingest.sock",
  "data_dir": ".gateway-runtime/gateway"
}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "controld.json"), []byte(`{
  "gateway_socket": ".gateway-runtime/gateway-control.sock",
  "telemetry_socket": ".gateway-runtime/telemetry-query.sock",
  "data_dir": ".gateway-runtime/control",
  "config_path": "configs/config.yaml"
}`), 0644); err != nil {
		t.Fatal(err)
	}

	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	specs := superviseProcessSpecs(configDir, runtimeRoot)
	if got := argValue(specs[0].args, "-data-dir"); got != filepath.Join(runtimeRoot, "telemetry-migrated") {
		t.Fatalf("telemetry data dir = %q", got)
	}
	if got := argValue(specs[1].args, "-control"); got != filepath.Join(runtimeRoot, "gateway-control.sock") {
		t.Fatalf("gateway control socket = %q", got)
	}
	if got := argValue(specs[2].args, "-telemetry"); got != filepath.Join(runtimeRoot, "telemetry-query.sock") {
		t.Fatalf("control telemetry socket = %q", got)
	}
	if got := argValue(specs[2].args, "-authoring-config"); got != filepath.Join(configDir, "config.yaml") {
		t.Fatalf("control authoring config = %q", got)
	}
}

func TestResolveConfigPathUsesConfigDir(t *testing.T) {
	etcDir := filepath.Join(string(os.PathSeparator), "etc", "ai-model-gateway")
	if got := resolveConfigPath("configs/config.yaml", etcDir, filepath.Join(etcDir, "controld.json"), "config.yaml"); got != filepath.Join(etcDir, "config.yaml") {
		t.Fatalf("installed configs/config.yaml resolved to %q", got)
	}

	dockerDir := filepath.Join(string(os.PathSeparator), "opt", "ai-model-gateway", "configs", "docker")
	if got := resolveConfigPath("configs/config.yaml", dockerDir, filepath.Join(dockerDir, "controld.json"), "config.yaml"); got != filepath.Join(filepath.Dir(dockerDir), "config.yaml") {
		t.Fatalf("docker configs/config.yaml resolved to %q", got)
	}

	if got := resolveConfigPath("../config.yaml", dockerDir, filepath.Join(dockerDir, "controld.json"), "config.yaml"); got != filepath.Join(filepath.Dir(dockerDir), "config.yaml") {
		t.Fatalf("relative docker config resolved to %q", got)
	}
}

func TestVerifyLocalBundleUsesManifestDirectory(t *testing.T) {
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
	manifest, err := release.BuildManifest(release.BuildOptions{Root: root})
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	manifestPath := filepath.Join(root, release.ManifestFileName)
	if err := release.SaveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	if err := verifyLocalBundle(manifestPath, true); err != nil {
		t.Fatalf("verifyLocalBundle() error = %v", err)
	}
}

func TestStopProcessesUsesPerChildTimeout(t *testing.T) {
	processes := []*runningProcess{
		{done: make(chan error)},
		{done: make(chan error)},
	}
	started := time.Now()
	stopProcessesWithTimeout(processes, time.Millisecond)
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("stopProcessesWithTimeout took %s, want short bounded wait", elapsed)
	}
}

func TestCopyBundlePayloadReplacesAdminDist(t *testing.T) {
	bundleRoot := t.TempDir()
	installDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleRoot, release.ManifestFileName), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	newAssets := filepath.Join(bundleRoot, "web", "admin", "dist", "assets")
	if err := os.MkdirAll(newAssets, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newAssets, "index-new.js"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	oldAssets := filepath.Join(installDir, "web", "admin", "dist", "assets")
	if err := os.MkdirAll(oldAssets, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldAssets, "index-old.js"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyBundlePayload(bundleRoot, installDir, release.Manifest{AdminDistHash: "present"}); err != nil {
		t.Fatalf("copyBundlePayload() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldAssets, "index-old.js")); !os.IsNotExist(err) {
		t.Fatalf("stale old asset still exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "web", "admin", "dist", "assets", "index-new.js")); err != nil {
		t.Fatalf("new asset missing: %v", err)
	}
}

func TestClientsPrint(t *testing.T) {
	dir := t.TempDir()
	gw := filepath.Join(dir, "gatewayd.json")
	if err := os.WriteFile(gw, []byte(`{"listen":"127.0.0.1:18080"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"aigw", "clients", "print", "-config-dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(clients print) code = %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "OPENAI_BASE_URL") || !strings.Contains(out, "ANTHROPIC_BASE_URL") {
		t.Fatalf("unexpected stdout:\n%s", out)
	}
}

func argValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}
