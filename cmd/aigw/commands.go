package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/release"
)

func runDoctor(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	runtimeRoot := fs.String("runtime-root", ".gateway-runtime", "runtime root")
	configDir := fs.String("config-dir", "configs", "config directory")
	manifestPath := fs.String("manifest", release.ManifestFileName, "bundle manifest path")
	format := fs.String("format", "text", "output format (text|json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report := map[string]any{
		"version":      Version,
		"runtime_root": *runtimeRoot,
		"config_dir":   *configDir,
		"checks":       []map[string]any{},
	}
	checks := make([]map[string]any, 0)
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": ok, "detail": detail})
	}
	for _, path := range []string{"gatewayd.json", "controld.json", "telemetryd.json"} {
		full := filepath.Join(*configDir, path)
		_, err := os.Stat(full)
		add("config:"+path, err == nil, errorDetail(err))
	}
	if manifest, err := release.LoadManifest(*manifestPath); err == nil {
		bundleRoot := filepath.Dir(filepath.Clean(*manifestPath))
		verify := release.VerifyManifest(bundleRoot, manifest)
		add("manifest", verify.OK, strings.Join(verify.Issues, "; "))
	} else {
		add("manifest", false, err.Error())
	}
	for _, path := range []string{
		filepath.Join(*runtimeRoot, "gateway-control.sock"),
		filepath.Join(*runtimeRoot, "telemetry-ingest.sock"),
		filepath.Join(*runtimeRoot, "telemetry-query.sock"),
	} {
		_, err := os.Stat(path)
		add("socket:"+filepath.Base(path), err == nil, errorDetail(err))
	}
	report["checks"] = checks
	return writeOutput(stdout, *format, report, func() error {
		fmt.Fprintf(stdout, "AI Model Gateway doctor v%s\n", Version)
		for _, check := range checks {
			status := "FAIL"
			if check["ok"].(bool) {
				status = "OK"
			}
			fmt.Fprintf(stdout, "%-8s %s", status, check["name"])
			if detail := strings.TrimSpace(fmt.Sprint(check["detail"])); detail != "" {
				fmt.Fprintf(stdout, " - %s", detail)
			}
			fmt.Fprintln(stdout)
		}
		return nil
	})
}

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	controlURL := fs.String("control-url", "http://127.0.0.1:18081", "control-plane URL")
	gatewayURL := fs.String("gateway-url", "http://127.0.0.1:18080", "gateway URL")
	token := fs.String("token", os.Getenv("ADMIN_TOKEN"), "admin token for runtime status")
	format := fs.String("format", "text", "output format (text|json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	status := map[string]any{
		"version": Version,
		"control": probeJSON(*controlURL+"/api/admin/runtime/status", *token),
		"gateway": probeJSON(*gatewayURL+"/-/health", ""),
	}
	return writeOutput(stdout, *format, status, func() error {
		fmt.Fprintf(stdout, "aigw %s\n", Version)
		printProbe(stdout, "control", status["control"].(map[string]any))
		printProbe(stdout, "gateway", status["gateway"].(map[string]any))
		return nil
	})
}

func runLogs(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	runtimeRoot := fs.String("runtime-root", ".gateway-runtime", "runtime root")
	lines := fs.Int("n", 120, "lines per log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"telemetryd", "gatewayd", "controld"}
	}
	for _, target := range targets {
		path := filepath.Join(*runtimeRoot, "logs", target+".log")
		fmt.Fprintf(stdout, "==> %s <==\n", path)
		content, err := tailFile(path, *lines)
		if err != nil {
			fmt.Fprintf(stdout, "%v\n", err)
			continue
		}
		_, _ = stdout.Write(content)
		if len(content) == 0 || content[len(content)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
	}
	return nil
}

func runBackup(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	runtimeRoot := fs.String("runtime-root", ".gateway-runtime", "runtime root")
	configDir := fs.String("config-dir", "configs", "config directory")
	outDir := fs.String("out", "", "backup directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outDir == "" {
		*outDir = filepath.Join(*runtimeRoot, "backups", time.Now().UTC().Format("20060102-150405"))
	}
	for _, item := range []struct {
		src string
		dst string
	}{
		{*configDir, filepath.Join(*outDir, "configs")},
		{filepath.Join(*runtimeRoot, "control"), filepath.Join(*outDir, "runtime", "control")},
		{filepath.Join(*runtimeRoot, "gateway"), filepath.Join(*outDir, "runtime", "gateway")},
		{filepath.Join(*runtimeRoot, "telemetry"), filepath.Join(*outDir, "runtime", "telemetry")},
		{filepath.Join(*runtimeRoot, "telemetry-migrated"), filepath.Join(*outDir, "runtime", "telemetry-migrated")},
		{release.ManifestFileName, filepath.Join(*outDir, release.ManifestFileName)},
	} {
		if err := copyPathIfExists(item.src, item.dst); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "backup written to %s\n", *outDir)
	return nil
}

func runService(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "print" {
		fmt.Fprint(stdout, `[Unit]
Description=AI Model Gateway
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/ai-model-gateway
ExecStart=/opt/ai-model-gateway/bin/aigw supervise -runtime-root /var/lib/ai-model-gateway -config-dir /etc/ai-model-gateway -bin-dir /opt/ai-model-gateway/bin -strict-manifest=true -manifest /opt/ai-model-gateway/aigw-manifest.json
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`)
		return nil
	}
	return fmt.Errorf("service install/remove is intentionally host-local; use 'aigw service print' and install with your service manager")
}

func probeJSON(rawURL string, token string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["ok"] = resp.StatusCode >= 200 && resp.StatusCode < 400
	body["status_code"] = resp.StatusCode
	return body
}

func printProbe(w io.Writer, name string, probe map[string]any) {
	if probe["ok"] == true {
		fmt.Fprintf(w, "%s: ok", name)
	} else {
		fmt.Fprintf(w, "%s: error", name)
	}
	if status, ok := probe["status"].(string); ok && status != "" {
		fmt.Fprintf(w, " status=%s", status)
	}
	if errText, ok := probe["error"].(string); ok && errText != "" {
		fmt.Fprintf(w, " error=%s", errText)
	}
	fmt.Fprintln(w)
}

func tailFile(path string, n int) ([]byte, error) {
	if n <= 0 {
		n = 120
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			copy(lines, lines[1:])
			lines = lines[:n]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
