package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// OutputFormat 输出格式类型
type OutputFormat string

const (
	FormatText OutputFormat = "text"
	FormatJSON OutputFormat = "json"
)

// OutputFormatter 输出格式化器
type OutputFormatter struct {
	format OutputFormat
	writer io.Writer
}

// NewOutputFormatter 创建输出格式化器
func NewOutputFormatter(format OutputFormat, writer io.Writer) *OutputFormatter {
	if format == "" {
		format = FormatText
	}
	return &OutputFormatter{
		format: format,
		writer: writer,
	}
}

// WriteJSON 写入 JSON 格式
func (f *OutputFormatter) WriteJSON(data interface{}) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// WriteTable 写入表格格式
func (f *OutputFormatter) WriteTable(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(f.writer, 0, 0, 2, ' ', 0)

	// 写入表头
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, h)
	}
	fmt.Fprintln(w)

	// 写入数据行
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, cell)
		}
		fmt.Fprintln(w)
	}

	return w.Flush()
}

// Write 写入数据（自动根据格式选择）
func (f *OutputFormatter) Write(data interface{}, tableFunc func() ([]string, [][]string)) error {
	switch f.format {
	case FormatJSON:
		return f.WriteJSON(data)
	case FormatText:
		if tableFunc != nil {
			headers, rows := tableFunc()
			return f.WriteTable(headers, rows)
		}
		return fmt.Errorf("table function required for text format")
	default:
		return fmt.Errorf("unsupported format: %s", f.format)
	}
}

// WriteText 写入纯文本
func (f *OutputFormatter) WriteText(text string) error {
	_, err := fmt.Fprint(f.writer, text)
	return err
}

// WriteTextf 写入格式化文本
func (f *OutputFormatter) WriteTextf(format string, args ...interface{}) error {
	_, err := fmt.Fprintf(f.writer, format, args...)
	return err
}

// WriteSuccess 写入成功标记
func (f *OutputFormatter) WriteSuccess(message string) error {
	_, err := fmt.Fprintf(f.writer, "✓ %s\n", message)
	return err
}

// WriteError 写入错误标记
func (f *OutputFormatter) WriteError(message string) error {
	_, err := fmt.Fprintf(f.writer, "✗ %s\n", message)
	return err
}

// BoolStatus 将布尔值转换为可读字符串
func BoolStatus(enabled bool) string {
	if enabled {
		return "✓"
	}
	return "✗"
}

// FormatBool 将布尔值转换为启用/禁用文本
func FormatBool(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
