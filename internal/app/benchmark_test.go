package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// BenchmarkJSONParsing_Stdlib benchmarks standard library JSON parsing
func BenchmarkJSONParsing_Stdlib(b *testing.B) {
	data := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req map[string]interface{}
		if err := json.Unmarshal(data, &req); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPipelineRequest benchmarks full pipeline request processing
func BenchmarkPipelineRequest(b *testing.B) {
	// Setup minimal pipeline
	// This is a placeholder - actual implementation depends on pipeline structure

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate request processing
	}
}

// BenchmarkBufferAllocation benchmarks buffer allocation
func BenchmarkBufferAllocation(b *testing.B) {
	b.Run("NoPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := make([]byte, 32*1024)
			_ = buf
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		pool := &sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get().([]byte)
			pool.Put(buf)
		}
	})
}

// BenchmarkStringAllocation benchmarks string operations
func BenchmarkStringAllocation(b *testing.B) {
	b.Run("ToLower", func(b *testing.B) {
		s := "Content-Type"
		for i := 0; i < b.N; i++ {
			_ = strings.ToLower(s)
		}
	})

	b.Run("DirectCompare", func(b *testing.B) {
		s := "Content-Type"
		for i := 0; i < b.N; i++ {
			_ = s[0] == 'C'
		}
	})
}

// BenchmarkBufferPool benchmarks our buffer pool implementation
func BenchmarkBufferPool(b *testing.B) {
	b.Run("Pooled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := GetBodyBuffer()
			_ = buf[:1024] // Simulate use
			PutBodyBuffer(buf)
		}
	})

	b.Run("Allocated", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := make([]byte, 32*1024)
			_ = buf[:1024]
		}
	})
}

// BenchmarkResponseBufferPool benchmarks response buffer pool
func BenchmarkResponseBufferPool(b *testing.B) {
	b.Run("Pooled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := GetResponseBuffer()
			buf.WriteString("test data")
			PutResponseBuffer(buf)
		}
	})

	b.Run("Allocated", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := new(bytes.Buffer)
			buf.WriteString("test data")
		}
	})
}

// BenchmarkBufferPoolParallel benchmarks buffer pool under concurrent access
func BenchmarkBufferPoolParallel(b *testing.B) {
	b.Run("Pooled", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := GetBodyBuffer()
				_ = buf[:1024]
				PutBodyBuffer(buf)
			}
		})
	})

	b.Run("Allocated", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := make([]byte, 32*1024)
				_ = buf[:1024]
			}
		})
	})
}

// BenchmarkTelemetryBatchWrite benchmarks telemetry batch writing
func BenchmarkTelemetryBatchWrite(b *testing.B) {
	// This requires store setup - simplified version
	b.Run("Batch256", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Simulate batch of 256 records
			batch := make([]int, 256)
			_ = batch
		}
	})

	b.Run("Batch64", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Simulate batch of 64 records
			batch := make([]int, 64)
			_ = batch
		}
	})
}

// BenchmarkJSONParsing_Fast benchmarks fast JSON parsing (json-iterator wrapper)
func BenchmarkJSONParsing_Fast(b *testing.B) {
	data := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req map[string]interface{}
		if err := JSONUnmarshal(data, &req); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONMarshal_Stdlib benchmarks standard library JSON marshaling
func BenchmarkJSONMarshal_Stdlib(b *testing.B) {
	data := map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
		"stream": true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONMarshal_Fast benchmarks fast JSON marshaling
func BenchmarkJSONMarshal_Fast(b *testing.B) {
	data := map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
		"stream": true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := JSONMarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtractModel benchmarks model extraction from JSON
func BenchmarkExtractModel(b *testing.B) {
	body := []byte(`{"model":"gpt-4-turbo-preview","messages":[{"role":"user","content":"hello"}]}`)

	b.Run("Stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var obj struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(body, &obj)
		}
	})

	b.Run("Fast", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var obj struct {
				Model string `json:"model"`
			}
			_ = JSONUnmarshal(body, &obj)
		}
	})
}

// BenchmarkExtractStream benchmarks stream field extraction
func BenchmarkExtractStream(b *testing.B) {
	body := []byte(`{"model":"gpt-4","stream":true}`)

	b.Run("Stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var obj struct {
				Stream bool `json:"stream"`
			}
			_ = json.Unmarshal(body, &obj)
		}
	})

	b.Run("Fast", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var obj struct {
				Stream bool `json:"stream"`
			}
			_ = JSONUnmarshal(body, &obj)
		}
	})
}

// BenchmarkTelemetryUsage benchmarks telemetry usage extraction
func BenchmarkTelemetryUsage(b *testing.B) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 50,
			"input_tokens": 100,
			"output_tokens": 50,
			"prompt_tokens_details": {"cached_tokens": 10},
			"input_tokens_details": {"cached_tokens": 10}
		}
	}`)

	b.Run("Stdlib", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var payload struct {
				Usage struct {
					PromptTokens     int64 `json:"prompt_tokens"`
					CompletionTokens int64 `json:"completion_tokens"`
					InputTokens      int64 `json:"input_tokens"`
					OutputTokens     int64 `json:"output_tokens"`
					PromptDetails    struct {
						CachedTokens int64 `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
					InputDetails struct {
						CachedTokens int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			_ = json.Unmarshal(body, &payload)
		}
	})

	b.Run("Fast", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var payload struct {
				Usage struct {
					PromptTokens     int64 `json:"prompt_tokens"`
					CompletionTokens int64 `json:"completion_tokens"`
					InputTokens      int64 `json:"input_tokens"`
					OutputTokens     int64 `json:"output_tokens"`
					PromptDetails    struct {
						CachedTokens int64 `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
					InputDetails struct {
						CachedTokens int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			_ = JSONUnmarshal(body, &payload)
		}
	})
}
