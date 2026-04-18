package cli

import (
	"bytes"
	"testing"
)

func TestOutputFormatter_WriteJSON(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatJSON, &buf)

	data := map[string]string{"key": "value"}
	err := formatter.WriteJSON(data)
	if err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	expected := `{
  "key": "value"
}
`
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestOutputFormatter_WriteTable(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	headers := []string{"NAME", "STATUS"}
	rows := [][]string{
		{"test", "active"},
		{"example", "inactive"},
	}

	err := formatter.WriteTable(headers, rows)
	if err != nil {
		t.Fatalf("WriteTable failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("NAME")) {
		t.Error("expected headers in output")
	}
	if !bytes.Contains([]byte(output), []byte("test")) {
		t.Error("expected row data in output")
	}
}

func TestOutputFormatter_WriteSuccess(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	err := formatter.WriteSuccess("Operation completed")
	if err != nil {
		t.Fatalf("WriteSuccess failed: %v", err)
	}

	expected := "✓ Operation completed\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestOutputFormatter_WriteError(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	err := formatter.WriteError("Operation failed")
	if err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}

	expected := "✗ Operation failed\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestBoolStatus(t *testing.T) {
	if BoolStatus(true) != "✓" {
		t.Error("expected ✓ for true")
	}
	if BoolStatus(false) != "✗" {
		t.Error("expected ✗ for false")
	}
}

func TestFormatBool(t *testing.T) {
	if FormatBool(true) != "enabled" {
		t.Error("expected 'enabled' for true")
	}
	if FormatBool(false) != "disabled" {
		t.Error("expected 'disabled' for false")
	}
}
