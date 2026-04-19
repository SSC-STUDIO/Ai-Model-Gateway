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

func TestNewOutputFormatter_EmptyFormat(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter("", &buf)
	if formatter.format != FormatText {
		t.Errorf("expected empty format to default to text, got %s", formatter.format)
	}
}

func TestOutputFormatter_Write_JSON(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatJSON, &buf)

	data := map[string]string{"key": "value"}
	err := formatter.Write(data, nil)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"key": "value"`)) {
		t.Errorf("expected JSON output, got %s", buf.String())
	}
}

func TestOutputFormatter_Write_TextWithTableFunc(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	data := "test"
	tableFunc := func() ([]string, [][]string) {
		return []string{"NAME"}, [][]string{{"test"}}
	}
	err := formatter.Write(data, tableFunc)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("NAME")) {
		t.Errorf("expected table output with headers, got %s", buf.String())
	}
}

func TestOutputFormatter_Write_TextWithoutTableFunc(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	err := formatter.Write("test", nil)
	if err == nil {
		t.Fatal("expected error when tableFunc is nil for text format")
	}
}

func TestOutputFormatter_Write_UnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	formatter := &OutputFormatter{format: "invalid", writer: &buf}

	err := formatter.Write("test", nil)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestOutputFormatter_WriteText(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	err := formatter.WriteText("hello world")
	if err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	if buf.String() != "hello world" {
		t.Errorf("expected 'hello world', got %q", buf.String())
	}
}

func TestOutputFormatter_WriteTextf(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewOutputFormatter(FormatText, &buf)

	err := formatter.WriteTextf("hello %s, count: %d", "world", 42)
	if err != nil {
		t.Fatalf("WriteTextf failed: %v", err)
	}
	if buf.String() != "hello world, count: 42" {
		t.Errorf("expected 'hello world, count: 42', got %q", buf.String())
	}
}
