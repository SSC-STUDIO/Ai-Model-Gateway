package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// MaxJSONBodySize 最大允许的 JSON 请求体大小 (10MB)
	MaxJSONBodySize = 10 * 1024 * 1024
	// MaxNestedDepth 最大嵌套深度
	MaxNestedDepth = 100
)

// SafeJSONDecoder 安全的 JSON 解码器
type SafeJSONDecoder struct {
	decoder *json.Decoder
}

// NewSafeJSONDecoder 创建安全的 JSON 解码器
func NewSafeJSONDecoder(r io.Reader) *SafeJSONDecoder {
	return &SafeJSONDecoder{
		decoder: json.NewDecoder(r),
	}
}

// Decode 安全地解码 JSON
func (d *SafeJSONDecoder) Decode(v interface{}) error {
	// 检查嵌套深度
	d.decoder.UseNumber()
	return d.decoder.Decode(v)
}

// ValidateJSONStructure 验证 JSON 结构是否合法
func ValidateJSONStructure(data []byte) error {
	// 检查大小限制
	if len(data) > MaxJSONBodySize {
		return fmt.Errorf("JSON body exceeds maximum size of %d bytes", MaxJSONBodySize)
	}

	// 检查嵌套深度
	depth := 0
	maxDepth := 0
	inString := false
	escape := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if escape {
			escape = false
			continue
		}

		if c == '\\' && inString {
			escape = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch c {
		case '{', '[':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
			if depth > MaxNestedDepth {
				return fmt.Errorf("JSON nesting depth exceeds maximum of %d", MaxNestedDepth)
			}
		case '}', ']':
			depth--
		}
	}

	if depth != 0 {
		return errors.New("invalid JSON structure: unbalanced brackets")
	}

	return nil
}

// SafeUnmarshal 安全地解析 JSON
func SafeUnmarshal(data []byte, v interface{}) error {
	if err := ValidateJSONStructure(data); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// LimitedReader 限制读取大小的 Reader
type LimitedReader struct {
	r     io.Reader
	limit int64
	n     int64
}

// NewLimitedReader 创建限制大小的 Reader
func NewLimitedReader(r io.Reader, limit int64) *LimitedReader {
	return &LimitedReader{
		r:     r,
		limit: limit,
	}
}

func (lr *LimitedReader) Read(p []byte) (n int, err error) {
	if lr.n >= lr.limit {
		return 0, fmt.Errorf("read limit of %d bytes exceeded", lr.limit)
	}

	remaining := lr.limit - lr.n
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err = lr.r.Read(p)
	lr.n += int64(n)
	return n, err
}

// ReadJSONBody 安全地读取和解析 JSON 请求体
func ReadJSONBody(r io.Reader, v interface{}) error {
	// 使用限制大小的 reader
	limitedReader := NewLimitedReader(r, MaxJSONBodySize)

	// 读取数据
	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)

	for {
		n, err := limitedReader.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			// 再次检查大小
			if len(data) > MaxJSONBodySize {
				return fmt.Errorf("request body exceeds maximum size of %d bytes", MaxJSONBodySize)
			}
		}
		if err != nil {
			if err.Error() == fmt.Sprintf("read limit of %d bytes exceeded", MaxJSONBodySize) {
				return fmt.Errorf("request body exceeds maximum size of %d bytes", MaxJSONBodySize)
			}
			if err == io.EOF {
				break
			}
			return err
		}
	}

	// 验证并解析 JSON
	return SafeUnmarshal(data, v)
}
