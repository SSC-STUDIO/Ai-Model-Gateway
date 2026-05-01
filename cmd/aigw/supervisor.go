package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"ai-model-gateway/internal/release"
)

var semverPattern = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?\b`)

type processSpec struct {
	name       string
	configPath string
	args       []string
}

type runningProcess struct {
	spec processSpec
	cmd  *exec.Cmd
	log  *os.File
	done chan error
}

func runSupervise(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runtimeRoot := fs.String("runtime-root", ".gateway-runtime", "runtime root")
	configDir := fs.String("config-dir", "configs", "config directory")
	binDir := fs.String("bin-dir", "", "binary directory (defaults to aigw binary directory, then PATH)")
	manifestPath := fs.String("manifest", release.ManifestFileName, "bundle manifest path")
	strictManifest := fs.Bool("strict-manifest", false, "fail when manifest is absent")
	startupTimeout := fs.Duration("startup-timeout", 30*time.Second, "startup health timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := verifyLocalBundle(*manifestPath, *strictManifest); err != nil {
		return err
	}
	if err := verifyDaemonBinaryVersions(*binDir); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	specs := superviseProcessSpecs(*configDir, *runtimeRoot)
	logDir := filepath.Join(*runtimeRoot, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	processes := make([]*runningProcess, 0, len(specs))
	childExited := make(chan string, 1)
	for _, spec := range specs {
		proc, err := startProcess(ctx, spec, *binDir, logDir)
		if err != nil {
			stopProcesses(processes)
			return err
		}
		processes = append(processes, proc)
		fmt.Fprintf(stdout, "started %s pid=%d log=%s\n", spec.name, proc.cmd.Process.Pid, proc.log.Name())
		go func(proc *runningProcess) {
			err := <-proc.done
			if err != nil {
				select {
				case childExited <- fmt.Sprintf("%s exited: %v", proc.spec.name, err):
				default:
				}
			}
		}(proc)
		if err := waitAfterStart(ctx, proc, *startupTimeout); err != nil {
			stopProcesses(processes)
			return err
		}
	}

	if err := waitForHTTP(ctx, "http://127.0.0.1:18081/-/health", *startupTimeout); err != nil {
		fmt.Fprintf(stderr, "warning: controld health check did not become healthy: %v\n", err)
	}
	fmt.Fprintln(stdout, "aigw supervise ready")

	select {
	case <-ctx.Done():
		fmt.Fprintln(stdout, "aigw supervise stopping")
	case message := <-childExited:
		stop()
		stopProcesses(processes)
		return errors.New(message)
	}
	stopProcesses(processes)
	return nil
}

func verifyLocalBundle(manifestPath string, strict bool) error {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		if strict {
			return fmt.Errorf("manifest path is required")
		}
		return nil
	}
	manifest, err := release.LoadManifest(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !strict {
			return nil
		}
		return fmt.Errorf("load manifest: %w", err)
	}
	// Use the manifest's directory as the bundle root for verification.
	// This allows aigw supervise to be invoked from any working directory
	// as long as -manifest and -bin-dir are specified with absolute paths.
	bundleRoot := filepath.Dir(filepath.Clean(manifestPath))
	report := release.VerifyManifest(bundleRoot, manifest)
	if !report.OK {
		return fmt.Errorf("refusing mixed or invalid bundle: %s", strings.Join(report.Issues, "; "))
	}
	return nil
}

func verifyDaemonBinaryVersions(binDir string) error {
	versions := make(map[string]string)
	for _, name := range []string{"telemetryd", "gatewayd", "controld"} {
		path, err := findBinary(name, binDir)
		if err != nil {
			return err
		}
		got, err := binaryVersion(path)
		if err != nil {
			return fmt.Errorf("%s version check failed: %w", name, err)
		}
		versions[name] = got
	}
	for name, got := range versions {
		if got != Version {
			return fmt.Errorf("refusing mixed daemon version: %s reports %s, aigw reports %s", name, got, Version)
		}
	}
	return nil
}

func superviseProcessSpecs(configDir string, runtimeRoot string) []processSpec {
	telemetryConfig := filepath.Join(configDir, "telemetryd.json")
	gatewayConfig := filepath.Join(configDir, "gatewayd.json")
	controlConfig := filepath.Join(configDir, "controld.json")
	return []processSpec{
		{
			name:       "telemetryd",
			configPath: telemetryConfig,
			args: []string{
				"-config", telemetryConfig,
				"-ingest", runtimePathForConfigValue(telemetryConfig, "ingest_socket", runtimeRoot, "telemetry-ingest.sock"),
				"-query", runtimePathForConfigValue(telemetryConfig, "query_socket", runtimeRoot, "telemetry-query.sock"),
				"-data-dir", runtimePathForConfigValue(telemetryConfig, "data_dir", runtimeRoot, "telemetry"),
			},
		},
		{
			name:       "gatewayd",
			configPath: gatewayConfig,
			args: []string{
				"-config", gatewayConfig,
				"-control", runtimePathForConfigValue(gatewayConfig, "control_socket", runtimeRoot, "gateway-control.sock"),
				"-telemetry", runtimePathForConfigValue(gatewayConfig, "telemetry_socket", runtimeRoot, "telemetry-ingest.sock"),
				"-data-dir", runtimePathForConfigValue(gatewayConfig, "data_dir", runtimeRoot, "gateway"),
			},
		},
		{
			name:       "controld",
			configPath: controlConfig,
			args: []string{
				"-config", controlConfig,
				"-gateway", runtimePathForConfigValue(controlConfig, "gateway_socket", runtimeRoot, "gateway-control.sock"),
				"-telemetry", runtimePathForConfigValue(controlConfig, "telemetry_socket", runtimeRoot, "telemetry-query.sock"),
				"-data-dir", runtimePathForConfigValue(controlConfig, "data_dir", runtimeRoot, "control"),
				"-authoring-config", authoringConfigPathForConfigValue(controlConfig, configDir),
			},
		},
	}
}

func runtimePathForConfigValue(configPath string, key string, runtimeRoot string, defaultLeaf string) string {
	raw := strings.TrimSpace(readBootstrapString(configPath, key))
	return resolveRuntimePath(raw, runtimeRoot, defaultLeaf)
}

func authoringConfigPathForConfigValue(configPath string, configDir string) string {
	raw := strings.TrimSpace(readBootstrapString(configPath, "config_path"))
	return resolveConfigPath(raw, configDir, configPath, "config.yaml")
}

func readBootstrapString(configPath string, key string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}

func resolveConfigPath(value string, configDir string, bootstrapConfigPath string, defaultLeaf string) string {
	root := authoringConfigRoot(configDir)
	if strings.TrimSpace(value) == "" {
		return filepath.Join(root, defaultLeaf)
	}
	cleanValue := filepath.Clean(value)
	if filepath.IsAbs(cleanValue) {
		return cleanValue
	}
	if cleanValue == "." || cleanValue == ".." || strings.HasPrefix(cleanValue, "."+string(os.PathSeparator)) || strings.HasPrefix(cleanValue, ".."+string(os.PathSeparator)) {
		return filepath.Clean(filepath.Join(filepath.Dir(bootstrapConfigPath), cleanValue))
	}
	parts := strings.Split(filepath.ToSlash(cleanValue), "/")
	if len(parts) > 1 && parts[0] == "configs" {
		cleanValue = filepath.Join(parts[1:]...)
	}
	return filepath.Join(root, cleanValue)
}

func authoringConfigRoot(configDir string) string {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		configDir = "configs"
	}
	cleanDir := filepath.Clean(configDir)
	if filepath.Base(cleanDir) == "docker" {
		return filepath.Dir(cleanDir)
	}
	return cleanDir
}

func resolveRuntimePath(value string, runtimeRoot string, defaultLeaf string) string {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		runtimeRoot = ".gateway-runtime"
	}
	if strings.TrimSpace(value) == "" {
		return filepath.Join(runtimeRoot, defaultLeaf)
	}
	cleanValue := filepath.Clean(value)
	defaultRoot := filepath.Clean(".gateway-runtime")
	if rel, err := filepath.Rel(defaultRoot, cleanValue); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.Join(runtimeRoot, rel)
	}
	return value
}

func startProcess(ctx context.Context, spec processSpec, binDir string, logDir string) (*runningProcess, error) {
	binary, err := findBinary(spec.name, binDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(spec.configPath); err != nil {
		return nil, fmt.Errorf("%s config %s: %w", spec.name, spec.configPath, err)
	}
	logPath := filepath.Join(logDir, spec.name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open %s log: %w", spec.name, err)
	}
	cmd := exec.CommandContext(ctx, binary, spec.args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start %s: %w", spec.name, err)
	}
	proc := &runningProcess{spec: spec, cmd: cmd, log: logFile, done: make(chan error, 1)}
	go func() {
		proc.done <- cmd.Wait()
		_ = logFile.Close()
	}()
	return proc, nil
}

func waitAfterStart(ctx context.Context, proc *runningProcess, timeout time.Duration) error {
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-proc.done:
		return fmt.Errorf("%s exited during startup: %w", proc.spec.name, err)
	case <-timer.C:
		return nil
	}
}

func stopProcesses(processes []*runningProcess) {
	stopProcessesWithTimeout(processes, 5*time.Second)
}

func stopProcessesWithTimeout(processes []*runningProcess, timeout time.Duration) {
	for i := len(processes) - 1; i >= 0; i-- {
		proc := processes[i]
		if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
			continue
		}
		_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	}
	for i := len(processes) - 1; i >= 0; i-- {
		proc := processes[i]
		if proc == nil {
			continue
		}
		timer := time.NewTimer(timeout)
		select {
		case <-proc.done:
			timer.Stop()
		case <-timer.C:
			if proc.cmd != nil && proc.cmd.Process != nil {
				_ = proc.cmd.Process.Kill()
			}
		}
	}
}

func findBinary(name string, binDir string) (string, error) {
	exeName := name
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	candidates := make([]string, 0, 3)
	if strings.TrimSpace(binDir) != "" {
		candidates = append(candidates, filepath.Join(binDir, exeName))
	}
	if self, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(self), exeName))
	}
	candidates = append(candidates, filepath.Join("bin", exeName))
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(exeName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("binary %s not found; set -bin-dir", name)
}

func binaryVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	match := semverPattern.FindString(string(out))
	if match == "" {
		return "", fmt.Errorf("could not parse version output %q", strings.TrimSpace(string(out)))
	}
	return match, nil
}

func waitForHTTP(ctx context.Context, rawURL string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ok := httpProbe(ctx, rawURL); ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", rawURL)
		case <-ticker.C:
		}
	}
}

func httpProbe(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}
