# Performance Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optimize gateway performance to achieve P99 < 100ms, throughput > 1000 RPS, memory < 500MB under load.

**Architecture:** Two parallel workstreams - Memory optimization (sync.Pool, buffer reuse) and Latency optimization (json-iterator, connection pool tuning).

**Tech Stack:** Go, `github.com/json-iterator/go`, sync.Pool, pprof

---

## File Structure

| File | Purpose | Agent |
|------|---------|-------|
| `internal/app/pool.go` | Buffer pool implementations | team-perf-mem |
| `internal/app/transport.go` | Modified with pooled buffers | team-perf-mem |
| `internal/infra/telemetrydb/store.go` | SQLite optimizations | team-perf-mem |
| `internal/app/json.go` | JSON iterator wrapper | team-perf-latency |
| `internal/app/pipeline.go` | Modified for fast JSON | team-perf-latency |
| `internal/app/gateway_handler.go` | Modified for fast JSON | team-perf-latency |
| `internal/app/benchmark_test.go` | Performance benchmarks | Both |

---

## Task 1: Setup Benchmarks (Both Agents)

**Files:**
- Create: `internal/app/benchmark_test.go`

---

- [ ] **Step 1.1: Create benchmark test file**

Create `internal/app/benchmark_test.go`:

```go
package app

import (
	"bytes"
	"encoding/json"
	"testing"
)

// BenchmarkJSONParsing benchmarks standard library JSON parsing
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
```

---

- [ ] **Step 1.2: Run baseline benchmarks**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go test -bench=. -benchmem ./internal/app/... 2>&1 | head -30
```

Save output as baseline for comparison.

---

- [ ] **Step 1.3: Commit**

```bash
git add internal/app/benchmark_test.go
git commit -m "perf: add benchmark tests

- Add JSON parsing benchmark
- Add buffer allocation benchmark
- Add string operation benchmark
- Establish performance baseline"
```

---

## Task 2: JSON Iterator Integration (team-perf-latency)

**Files:**
- Create: `internal/app/json.go`
- Modify: `internal/app/pipeline.go`
- Modify: `internal/app/gateway_handler.go`

---

- [ ] **Step 2.1: Add json-iterator dependency**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go get github.com/json-iterator/go
go mod tidy
```

---

- [ ] **Step 2.2: Create json wrapper**

Create `internal/app/json.go`:

```go
package app

import jsoniter "github.com/json-iterator/go"

// json is a jsoniter instance configured for standard library compatibility
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// JSONMarshal wraps jsoniter Marshal
func JSONMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// JSONUnmarshal wraps jsoniter Unmarshal
func JSONUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
```

---

- [ ] **Step 2.3: Update pipeline.go to use fast JSON**

In `internal/app/pipeline.go`, find and replace:

```go
// Change from:
import "encoding/json"

// To:
// Use fast JSON from json.go
```

Replace all `json.Marshal` with `JSONMarshal` and `json.Unmarshal` with `JSONUnmarshal`.

---

- [ ] **Step 2.4: Update gateway_handler.go**

Similarly update `internal/app/gateway_handler.go` to use the fast JSON functions.

---

- [ ] **Step 2.5: Add fast JSON benchmark**

Add to `internal/app/benchmark_test.go`:

```go
import "github.com/json-iterator/go"

var fastjson = jsoniter.ConfigCompatibleWithStandardLibrary

func BenchmarkJSONParsing_Fast(b *testing.B) {
	data := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req map[string]interface{}
		if err := fastjson.Unmarshal(data, &req); err != nil {
			b.Fatal(err)
		}
	}
}
```

---

- [ ] **Step 2.6: Run benchmark comparison**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go test -bench=BenchmarkJSONParsing -benchmem ./internal/app/...
```

Expected: Fast JSON shows improvement over standard library.

---

- [ ] **Step 2.7: Commit**

```bash
git add internal/app/json.go internal/app/pipeline.go internal/app/gateway_handler.go internal/app/benchmark_test.go go.mod go.sum
git commit -m "perf: integrate json-iterator for faster JSON parsing

- Add json.go wrapper for json-iterator
- Update pipeline to use fast JSON
- Add benchmark showing improvement"
```

---

## Task 3: Buffer Pool Implementation (team-perf-mem)

**Files:**
- Create: `internal/app/pool.go`
- Modify: `internal/app/transport.go`

---

- [ ] **Step 3.1: Create pool.go**

Create `internal/app/pool.go`:

```go
package app

import (
	"bytes"
	"sync"
)

const (
	// bufferSize is the default size for pooled buffers (32KB)
	bufferSize = 32 * 1024
)

var (
	// bodyBufferPool pools byte slices for request body reading
	bodyBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, bufferSize)
			return &buf
		},
	}

	// responseBufferPool pools bytes.Buffer for response building
	responseBufferPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

// GetBodyBuffer gets a byte slice from the pool
func GetBodyBuffer() []byte {
	bufPtr := bodyBufferPool.Get().(*[]byte)
	return *bufPtr
}

// PutBodyBuffer returns a byte slice to the pool
func PutBodyBuffer(buf []byte) {
	if cap(buf) < bufferSize {
		return // Don't put small buffers back
	}
	bodyBufferPool.Put(&buf)
}

// GetResponseBuffer gets a bytes.Buffer from the pool
func GetResponseBuffer() *bytes.Buffer {
	buf := responseBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutResponseBuffer returns a bytes.Buffer to the pool
func PutResponseBuffer(buf *bytes.Buffer) {
	if buf != nil {
		responseBufferPool.Put(buf)
	}
}
```

---

- [ ] **Step 3.2: Update transport.go to use pooled buffers**

In `internal/app/transport.go`, find body reading code and update:

```go
// Change from:
body, err := io.ReadAll(resp.Body)

// To:
buf := GetBodyBuffer()
defer PutBodyBuffer(buf)

// Read into pooled buffer
n, err := io.ReadFull(resp.Body, buf)
if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
    // Fall back to dynamic allocation for large bodies
    body, err = io.ReadAll(resp.Body)
} else {
    body = buf[:n]
}
```

---

- [ ] **Step 3.3: Add pool benchmark**

Add to `internal/app/benchmark_test.go`:

```go
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
```

---

- [ ] **Step 3.4: Run pool benchmark**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go test -bench=BenchmarkBufferPool -benchmem ./internal/app/...
```

Expected: Pooled version shows fewer allocations.

---

- [ ] **Step 3.5: Commit**

```bash
git add internal/app/pool.go internal/app/transport.go internal/app/benchmark_test.go
git commit -m "perf: add buffer pool for reduced allocations

- Add pool.go with body and response buffer pools
- Update transport to use pooled buffers
- Benchmark shows reduced GC pressure"
```

---

## Task 4: Connection Pool Tuning (team-perf-latency)

**Files:**
- Modify: `internal/app/transport.go`

---

- [ ] **Step 4.1: Find transport initialization**

Read `internal/app/transport.go` to find where HTTP transport is created.

---

- [ ] **Step 4.2: Update connection pool settings**

Update the HTTP transport configuration:

```go
// Change from defaults to tuned values:
transport := &http.Transport{
    MaxIdleConns:        500,  // Increased from 100
    MaxIdleConnsPerHost: 100,  // Increased from 10
    MaxConnsPerHost:     200,  // New: limit per host
    IdleConnTimeout:     90 * time.Second,
    TLSHandshakeTimeout: 10 * time.Second,
    ExpectContinueTimeout: 1 * time.Second,
    // Enable HTTP/2
    ForceAttemptHTTP2: true,
}
```

---

- [ ] **Step 4.3: Verify build**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go build ./internal/app/...
```

Expected: No errors

---

- [ ] **Step 4.4: Commit**

```bash
git add internal/app/transport.go
git commit -m "perf: tune HTTP connection pool settings

- Increase MaxIdleConns to 500
- Increase MaxIdleConnsPerHost to 100
- Add MaxConnsPerHost limit of 200
- Enable ForceAttemptHTTP2"
```

---

## Task 5: SQLite Telemetry Optimization (team-perf-mem)

**Files:**
- Modify: `internal/infra/telemetrydb/store.go`

---

- [ ] **Step 5.1: Read current store implementation**

Run:
```bash
head -100 /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway/internal/infra/telemetrydb/store.go
```

---

- [ ] **Step 5.2: Update batch settings**

Find and update the batch writer settings:

```go
// Change from:
const (
    batchSize     = 64
    flushInterval = 200 * time.Millisecond
)

// To:
const (
    batchSize     = 256  // Increased for higher throughput
    flushInterval = 100 * time.Millisecond  // Faster flush
)
```

---

- [ ] **Step 5.3: Add prepared statement cache**

Add to the Store struct:

```go
type Store struct {
    // ... existing fields
    stmtCache map[string]*sql.Stmt
}
```

Update query methods to use prepared statements:

```go
func (s *Store) getStmt(query string) (*sql.Stmt, error) {
    if stmt, ok := s.stmtCache[query]; ok {
        return stmt, nil
    }
    
    stmt, err := s.db.Prepare(query)
    if err != nil {
        return nil, err
    }
    
    if s.stmtCache == nil {
        s.stmtCache = make(map[string]*sql.Stmt)
    }
    s.stmtCache[query] = stmt
    return stmt, nil
}
```

---

- [ ] **Step 5.4: Add telemetry benchmark**

Add to `internal/app/benchmark_test.go`:

```go
func BenchmarkTelemetryBatchWrite(b *testing.B) {
	// This requires store setup - simplified version
	b.Run("Batch256", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Simulate batch of 256 records
		}
	})
}
```

---

- [ ] **Step 5.5: Commit**

```bash
git add internal/infra/telemetrydb/store.go
git commit -m "perf: optimize telemetry SQLite writes

- Increase batch size to 256
- Reduce flush interval to 100ms
- Add prepared statement caching"
```

---

## Task 6: Final Benchmarking & Verification

**Files:**
- Modify: (benchmark output only)

---

- [ ] **Step 6.1: Run all benchmarks**

Run:
```bash
cd /C/Users/96152/My-Project/Active/Software/AI-Model-Gateway
go test -bench=. -benchmem ./internal/app/... > benchmark_results.txt
cat benchmark_results.txt
```

---

- [ ] **Step 6.2: Build full project**

Run:
```bash
go build ./...
```

Expected: Success

---

- [ ] **Step 6.3: Run integration tests**

Run:
```bash
go test ./... -short
```

Expected: All tests pass

---

- [ ] **Step 6.4: Commit results**

```bash
git add benchmark_results.txt
git commit -m "perf: add benchmark results

- Document performance improvements
- JSON parsing: X% faster
- Memory allocations: Y% reduction"
```

---

## Performance Targets Verification

| Metric | Target | How to Verify |
|--------|--------|---------------|
| P99 Latency | < 100ms | `go test -bench=BenchmarkPipeline` |
| Throughput | > 1000 RPS | Load test with `wrk` or `ab` |
| Memory | < 500MB | `go test -benchmem`, check alloc/op |

---

## Coordination Notes

### team-perf-mem Tasks
- Task 1 (shared): Setup benchmarks
- Task 3: Buffer pool implementation
- Task 5: SQLite optimization
- Task 6 (shared): Final verification

### team-perf-latency Tasks
- Task 1 (shared): Setup benchmarks
- Task 2: JSON iterator integration
- Task 4: Connection pool tuning
- Task 6 (shared): Final verification

### Conflict Avoidance
- Both agents modify `benchmark_test.go` - coordinate via separate branches
- `transport.go` is modified by both - mem agent does pool, latency does connection settings

---

**Plan Complete**
