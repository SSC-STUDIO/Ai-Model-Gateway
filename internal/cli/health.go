package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func checkHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	endpoint := fs.String("endpoint", "http://127.0.0.1:18080/-/health", "Health check endpoint")
	timeout := fs.Duration("timeout", 5*time.Second, "Request timeout")
	
	if err := fs.Parse(args); err != nil {
		return err
	}

	client := &http.Client{
		Timeout: *timeout,
	}

	resp, err := client.Get(*endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Health check failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "✗ Health check failed: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		// Not JSON, but HTTP 200 is OK
		fmt.Println("✓ Gateway is healthy")
		return nil
	}

	fmt.Println("✓ Gateway is healthy")
	
	if status, ok := health["status"].(string); ok {
		fmt.Printf("  Status: %s\n", status)
	}
	if strategy, ok := health["router_strategy"].(string); ok {
		fmt.Printf("  Router: %s\n", strategy)
	}
	if bridge, ok := health["bridge"].(map[string]interface{}); ok {
		enabled, _ := bridge["enabled"].(bool)
		fmt.Printf("  Bridge: %v\n", enabled)
	}
	if models, ok := health["available_models"].([]interface{}); ok {
		fmt.Printf("  Models: %d available\n", len(models))
	}
	if upstreams, ok := health["upstreams"].(map[string]interface{}); ok {
		fmt.Printf("  Upstreams: %d configured\n", len(upstreams))
	}

	return nil
}
