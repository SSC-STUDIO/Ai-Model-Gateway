package httpserver

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// CompressionMiddleware provides HTTP response compression.
// It buffers the response body and compresses it with gzip when the body
// exceeds MinBytes and the content type is not text/event-stream.
// Brotli is not supported because the brotli library is not in go.mod.
type CompressionMiddleware struct {
	minBytes  int
	gzipLevel int
}

// compressionResponseWriter wraps http.ResponseWriter to buffer and
// conditionally compress the response.
type compressionResponseWriter struct {
	http.ResponseWriter
	buf         *bytes.Buffer
	minBytes    int
	gzipLevel   int
	compressed  bool
	headers     http.Header
	statusCode  int
	wroteHeader bool
}

// NewCompressionMiddleware creates a compression middleware.
// minBytes sets the minimum response body size to compress.
// level configures the gzip compression level: "fast" (1), "default" (-1),
// "best" (9). Any other value falls back to default.
func NewCompressionMiddleware(minBytes int, level string) *CompressionMiddleware {
	return &CompressionMiddleware{
		minBytes:  minBytes,
		gzipLevel: parseGzipLevel(level),
	}
}

// parseGzipLevel maps a human-readable level name to a gzip constant.
func parseGzipLevel(level string) int {
	switch strings.ToLower(level) {
	case "fast":
		return gzip.BestSpeed
	case "best":
		return gzip.BestCompression
	default:
		return gzip.DefaultCompression
	}
}

// Wrap returns an http.Handler that applies compression to eligible responses.
func (m *CompressionMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}

		cw := &compressionResponseWriter{
			ResponseWriter: w,
			buf:            &bytes.Buffer{},
			minBytes:       m.minBytes,
			gzipLevel:      m.gzipLevel,
			headers:        make(http.Header),
		}

		next.ServeHTTP(cw, r)
		cw.finish()
	})
}

// acceptsGzip reports whether the Accept-Encoding header contains gzip.
func acceptsGzip(acceptEncoding string) bool {
	for _, enc := range strings.Split(acceptEncoding, ",") {
		enc = strings.TrimSpace(enc)
		if enc == "gzip" || strings.HasPrefix(enc, "gzip;") {
			return true
		}
	}
	return false
}

// Header returns the underlying header map so that handlers can set headers
// before the response is written.
func (cw *compressionResponseWriter) Header() http.Header {
	return cw.headers
}

// WriteHeader captures the status code without writing it to the underlying
// ResponseWriter yet, because we may need to alter headers for compression.
func (cw *compressionResponseWriter) WriteHeader(code int) {
	cw.statusCode = code
	cw.wroteHeader = true
}

// Write appends data to the internal buffer. The actual response is deferred
// until finish() is called.
func (cw *compressionResponseWriter) Write(p []byte) (int, error) {
	if !cw.wroteHeader {
		cw.statusCode = http.StatusOK
		cw.wroteHeader = true
	}
	return cw.buf.Write(p)
}

// finish decides whether to compress the buffered body and writes the
// complete response to the underlying ResponseWriter.
func (cw *compressionResponseWriter) finish() {
	if cw.buf.Len() == 0 && cw.statusCode == 0 {
		copyHeaders(cw.ResponseWriter.Header(), cw.headers)
		cw.ResponseWriter.WriteHeader(http.StatusOK)
		return
	}

	contentType := cw.headers.Get("Content-Type")

	// Skip compression for SSE streams.
	if strings.HasPrefix(contentType, "text/event-stream") {
		cw.writeUncompressed()
		return
	}

	body := cw.buf.Bytes()

	// Compress only if the body meets the minimum size threshold.
	if cw.buf.Len() >= cw.minBytes {
		var compressed bytes.Buffer
		gw, err := gzip.NewWriterLevel(&compressed, cw.gzipLevel)
		if err != nil {
			cw.writeUncompressed()
			return
		}
		if _, err := gw.Write(body); err != nil {
			gw.Close()
			cw.writeUncompressed()
			return
		}
		gw.Close()

		cw.compressed = true
		copyHeaders(cw.ResponseWriter.Header(), cw.headers)
		cw.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		cw.ResponseWriter.Header().Set("Vary", "Accept-Encoding")
		cw.ResponseWriter.Header().Del("Content-Length")
		status := cw.statusCode
		if status == 0 {
			status = http.StatusOK
		}
		cw.ResponseWriter.WriteHeader(status)
		cw.ResponseWriter.Write(compressed.Bytes())
		return
	}

	cw.writeUncompressed()
}

// writeUncompressed forwards the buffered body without compression.
func (cw *compressionResponseWriter) writeUncompressed() {
	copyHeaders(cw.ResponseWriter.Header(), cw.headers)
	status := cw.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	cw.ResponseWriter.WriteHeader(status)
	cw.ResponseWriter.Write(cw.buf.Bytes())
}

// copyHeaders copies all headers from src to dst.
func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		dst[k] = vv
	}
}

// ReadFrom implements io.ReaderFrom to prevent the standard library from
// using sendfile when a compressionResponseWriter is in play; we need to
// capture all writes into the buffer.
func (cw *compressionResponseWriter) ReadFrom(r io.Reader) (n int64, err error) {
	return io.Copy(cw.buf, r)
}
