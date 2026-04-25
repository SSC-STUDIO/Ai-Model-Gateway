package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-model-gateway/internal/cli"
)

type HealthCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewHealthCommand(client *cli.ControlPlaneClient, output io.Writer) *HealthCommand {
	return &HealthCommand{client: client, output: output}
}

func (c *HealthCommand) Check(ctx context.Context, format string) error {
	start := time.Now()

	status, err := c.client.GetStatus(ctx)
	if err != nil {
		if format == "json" {
			return json.NewEncoder(c.output).Encode(map[string]interface{}{
				"healthy": false,
				"error":   err.Error(),
			})
		}
		return fmt.Errorf("health check failed: %w", err)
	}

	latency := time.Since(start).Milliseconds()

	gatewayReady := status.GatewayStatus == "connected"
	if status.Gateway != nil && status.Gateway.Readiness != "" {
		gatewayReady = gatewayReady && status.Gateway.Readiness == "ready"
	}
	healthy := gatewayReady && status.TelemetryStatus == "connected"

	if format == "json" {
		return json.NewEncoder(c.output).Encode(map[string]interface{}{
			"healthy":          healthy,
			"gateway_status":   status.GatewayStatus,
			"telemetry_status": status.TelemetryStatus,
			"gateway_readiness": func() string {
				if status.Gateway == nil {
					return ""
				}
				return status.Gateway.Readiness
			}(),
			"latency_ms": latency,
			"version":    status.Version,
		})
	}

	if healthy {
		fmt.Fprintf(c.output, "Status: HEALTHY\n")
	} else {
		fmt.Fprintf(c.output, "Status: UNHEALTHY\n")
	}
	fmt.Fprintf(c.output, "Gateway: %s\n", status.GatewayStatus)
	fmt.Fprintf(c.output, "Telemetry: %s\n", status.TelemetryStatus)
	if status.Gateway != nil && status.Gateway.Readiness != "" {
		fmt.Fprintf(c.output, "Readiness: %s\n", status.Gateway.Readiness)
	}
	fmt.Fprintf(c.output, "Latency: %dms\n", latency)
	fmt.Fprintf(c.output, "Version: %s\n", status.Version)

	if !healthy {
		return fmt.Errorf("system unhealthy")
	}
	return nil
}

func (c *HealthCommand) QuickCheck(ctx context.Context) error {
	status, err := c.client.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get control-plane status: %w", err)
	}
	if status.Gateway == nil {
		return fmt.Errorf("gateway status unavailable")
	}

	healthURL, err := gatewayHealthURL(status.Gateway.Listener, c.client.BaseURL())
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	fmt.Fprintln(c.output, "OK")
	return nil
}

func gatewayHealthURL(listener, controlPlaneURL string) (string, error) {
	listener = strings.TrimSpace(listener)
	if listener == "" {
		return "", fmt.Errorf("gateway listener unavailable")
	}

	if strings.Contains(listener, "://") {
		u, err := url.Parse(listener)
		if err != nil {
			return "", fmt.Errorf("parse gateway listener %q: %w", listener, err)
		}
		u.Path = "/-/health"
		u.RawQuery = ""
		u.Fragment = ""
		return u.String(), nil
	}

	base, err := url.Parse(controlPlaneURL)
	if err != nil {
		return "", fmt.Errorf("parse control-plane url %q: %w", controlPlaneURL, err)
	}
	controlHost := strings.TrimSpace(base.Hostname())
	if controlHost == "" {
		controlHost = "127.0.0.1"
	}

	host, port, err := splitListenerHostPort(listener)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" || isLoopbackHost(host) {
		host = controlHost
	}

	u := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/-/health",
	}
	return u.String(), nil
}

func splitListenerHostPort(listener string) (string, string, error) {
	if strings.HasPrefix(listener, ":") {
		return "", strings.TrimPrefix(listener, ":"), nil
	}
	if host, port, err := net.SplitHostPort(listener); err == nil {
		return strings.Trim(host, "[]"), port, nil
	}
	if !strings.Contains(listener, ":") {
		return listener, "80", nil
	}
	return "", "", fmt.Errorf("parse gateway listener %q: expected host:port", listener)
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
