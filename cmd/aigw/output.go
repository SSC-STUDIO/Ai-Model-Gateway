package main

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeOutput(w io.Writer, format string, value any, textFn func() error) error {
	switch format {
	case "", "text":
		return textFn()
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}
