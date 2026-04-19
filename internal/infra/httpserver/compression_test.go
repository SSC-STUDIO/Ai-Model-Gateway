package httpserver

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// largeBody returns a string that is definitely larger than typical minBytes.
func largeBody(n int) string {
	return strings.Repeat("Hello, World! This is a compression test payload. ", n)
}

func TestAcceptsGzip(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"gzip, deflate", true},
		{"deflate, gzip", true},
		{"gzip; q=0.9", true},
		{"deflate", false},
		{"", false},
		{"identity", false},
		{"*", false},
	}

	for _, tc := range tests {
		got := acceptsGzip(tc.header)
		if got != tc.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}


func TestCompressionMiddleware_CompressesLargeResponse(t *testing.T) {
	body := largeBody(50)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("expected Vary Accept-Encoding, got %q", rec.Header().Get("Vary"))
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to read gzip body: %v", err)
	}
	if string(decompressed) != body {
		t.Errorf("decompressed body mismatch: got %d bytes, want %d bytes", len(decompressed), len(body))
	}
}

func TestCompressionMiddleware_SkipsSmallResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "small")
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not compress small response")
	}
	if rec.Body.String() != "small" {
		t.Errorf("body mismatch: got %q, want %q", rec.Body.String(), "small")
	}
}

func TestCompressionMiddleware_SkipsSSE(t *testing.T) {
	body := largeBody(50)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not compress SSE stream")
	}
	if rec.Body.String() != body {
		t.Errorf("body mismatch: got %d bytes, want %d bytes", len(rec.Body.String()), len(body))
	}
}

func TestCompressionMiddleware_NoGzipAcceptEncoding(t *testing.T) {
	body := largeBody(50)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not compress when client does not accept gzip")
	}
	if rec.Body.String() != body {
		t.Errorf("body mismatch: got %d bytes, want %d bytes", len(rec.Body.String()), len(body))
	}
}

func TestCompressionMiddleware_StatusCodePreserved(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"not found"}`)
	})

	m := NewCompressionMiddleware(1, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status code mismatch: got %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_DefaultStatusCode(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, largeBody(50))
	})

	m := NewCompressionMiddleware(1, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCompressionMiddleware_HeadersPreserved(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Custom", "test-value")
		io.WriteString(w, largeBody(50))
	})

	m := NewCompressionMiddleware(1, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("X-Custom") != "test-value" {
		t.Errorf("custom header not preserved: got %q", rec.Header().Get("X-Custom"))
	}
}

func TestCompressionMiddleware_FastLevel(t *testing.T) {
	body := largeBody(50)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(1, 1)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(decompressed) != body {
		t.Error("decompressed body mismatch")
	}
}

func TestCompressionMiddleware_BestLevel(t *testing.T) {
	body := largeBody(50)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(1, 9)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(decompressed) != body {
		t.Error("decompressed body mismatch")
	}
}

func TestCompressionMiddleware_ExactMinBytes(t *testing.T) {
	body := strings.Repeat("a", 100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip for body exactly at minBytes, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_OneBelowMinBytes(t *testing.T) {
	body := strings.Repeat("a", 99)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not compress body below minBytes")
	}
	if rec.Body.String() != body {
		t.Error("body should be passed through uncompressed")
	}
}

func TestCompressionMiddleware_MultipleWrites(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, largeBody(20))
		io.WriteString(w, largeBody(20))
	})

	m := NewCompressionMiddleware(100, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	expected := largeBody(20) + largeBody(20)
	if string(decompressed) != expected {
		t.Errorf("body mismatch: got %d bytes, want %d bytes", len(decompressed), len(expected))
	}
}

func TestCompressionMiddleware_EmptyBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNoContent)
	})

	m := NewCompressionMiddleware(1, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

func TestCompressionMiddleware_CompressedSizeSmaller(t *testing.T) {
	body := largeBody(100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	})

	m := NewCompressionMiddleware(1, 0)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	m.Wrap(handler).ServeHTTP(rec, req)

	if rec.Body.Len() >= len(body) {
		t.Errorf("compressed body (%d bytes) should be smaller than original (%d bytes)", rec.Body.Len(), len(body))
	}
}

func TestCompressionMiddleware_CopyHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "text/plain")
	src.Set("X-Foo", "bar")

	dst := http.Header{}
	copyHeaders(dst, src)

	if dst.Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type not copied")
	}
	if dst.Get("X-Foo") != "bar" {
		t.Errorf("X-Foo not copied")
	}
}

func TestCompressionMiddleware_ReadFrom(t *testing.T) {
	cw := &compressionResponseWriter{
		buf: &bytes.Buffer{},
	}

	data := strings.NewReader("hello from reader")
	n, err := cw.ReadFrom(data)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if n != int64(len("hello from reader")) {
		t.Errorf("ReadFrom returned %d bytes, want %d", n, len("hello from reader"))
	}
	if cw.buf.String() != "hello from reader" {
		t.Errorf("buffer mismatch: got %q", cw.buf.String())
	}
}
