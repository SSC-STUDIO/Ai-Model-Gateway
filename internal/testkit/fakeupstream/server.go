package fakeupstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

type CapturedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type StreamChunk struct {
	Data  string
	Delay time.Duration
}

type Response struct {
	Delay      time.Duration
	StatusCode int
	Headers    http.Header
	Body       []byte
	Stream     []StreamChunk
}

type Handler func(CapturedRequest) Response

type Server struct {
	http *httptest.Server

	mu       sync.Mutex
	requests []CapturedRequest
	handler  Handler
}

func New(handler Handler) *Server {
	s := &Server{handler: handler}
	s.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		req := CapturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Clone(),
			Body:   append([]byte(nil), body...),
		}

		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		resp := Response{StatusCode: http.StatusOK}
		if s.handler != nil {
			resp = s.handler(req)
		}
		if resp.StatusCode == 0 {
			resp.StatusCode = http.StatusOK
		}
		if resp.Delay > 0 {
			time.Sleep(resp.Delay)
		}
		for key, values := range resp.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		if len(resp.Stream) > 0 {
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "text/event-stream")
			}
			w.WriteHeader(resp.StatusCode)
			flusher, _ := w.(http.Flusher)
			for _, chunk := range resp.Stream {
				if chunk.Delay > 0 {
					time.Sleep(chunk.Delay)
				}
				_, _ = io.WriteString(w, chunk.Data)
				if flusher != nil {
					flusher.Flush()
				}
			}
			return
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(resp.StatusCode)
		if len(resp.Body) > 0 {
			_, _ = w.Write(resp.Body)
		}
	}))
	return s
}

func (s *Server) URL() string {
	if s == nil || s.http == nil {
		return ""
	}
	return s.http.URL
}

func (s *Server) Close() {
	if s != nil && s.http != nil {
		s.http.Close()
	}
}

func (s *Server) Requests() []CapturedRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CapturedRequest, len(s.requests))
	copy(out, s.requests)
	for i := range out {
		out[i].Header = out[i].Header.Clone()
		out[i].Body = append([]byte(nil), out[i].Body...)
	}
	return out
}
