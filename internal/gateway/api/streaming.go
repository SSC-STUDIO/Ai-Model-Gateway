package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

// handleStreamResponse writes a streaming response.
func handleStreamResponse(w http.ResponseWriter, statusCode int, contentType string, respBody io.ReadCloser) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if strings.TrimSpace(contentType) == "" {
		contentType = "text/event-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(statusCode)

	flusher, _ := w.(http.Flusher)
	return copyStreamingBody(w, respBody, flusher)
}

func copyStreamingBody(w http.ResponseWriter, body io.ReadCloser, flusher http.Flusher) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if body == nil {
		return 0, 0, 0
	}
	defer body.Close()

	reader := bufio.NewReader(body)
	var eventData bytes.Buffer
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := w.Write(line); writeErr != nil {
				// Client disconnected — stop streaming.
				body.Close()
				return promptTokens, cachedPromptTokens, completionTokens
			}
			if flusher != nil {
				flusher.Flush()
			}

			trimmedLine := strings.TrimRight(string(line), "\r\n")
			if strings.TrimSpace(trimmedLine) == "" {
				if pt, cpt, ct, ok := extractUsageFromSSEEvent(eventData.Bytes()); ok {
					promptTokens, cachedPromptTokens, completionTokens = pt, cpt, ct
				}
				eventData.Reset()
			} else if strings.HasPrefix(trimmedLine, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(data)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if pt, cpt, ct, ok := extractUsageFromSSEEvent(eventData.Bytes()); ok {
					promptTokens, cachedPromptTokens, completionTokens = pt, cpt, ct
				}
			}
			return promptTokens, cachedPromptTokens, completionTokens
		}
	}
}

func handleStartedStreamResponse(w http.ResponseWriter, respBody io.ReadCloser, flusher http.Flusher) (promptTokens, cachedPromptTokens, completionTokens int64) {
	return copyStreamingBody(w, respBody, flusher)
}

var streamRetryHeartbeatInterval = 5 * time.Second

type streamRetrySession struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopper sync.Once
}

func safeFlush(flusher http.Flusher) (ok bool) {
	ok = true
	if flusher == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	flusher.Flush()
	return true
}

func safeWrite(writer http.ResponseWriter, data []byte) (written int, ok bool) {
	ok = true
	if writer == nil {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			written = 0
			ok = false
		}
	}()
	n, err := writer.Write(data)
	if err != nil {
		return n, false
	}
	return n, true
}

func startStreamRetrySession(w http.ResponseWriter) *streamRetrySession {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}

	session := &streamRetrySession{
		writer:  w,
		flusher: flusher,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if _, ok := safeWrite(w, []byte(": aigw waiting for upstream\n\n")); !ok {
		return nil
	}
	if !safeFlush(flusher) {
		return nil
	}

	go session.heartbeatLoop()
	return session
}

func (s *streamRetrySession) heartbeatLoop() {
	defer close(s.doneCh)

	interval := streamRetryHeartbeatInterval
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if _, ok := safeWrite(s.writer, []byte(": aigw keep-alive\n\n")); !ok {
				return
			}
			if !safeFlush(s.flusher) {
				return
			}
		}
	}
}

func (s *streamRetrySession) Stop() {
	if s == nil {
		return
	}
	s.stopper.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

// nonStreamKeepAliveInterval is the heartbeat interval for non-streaming keep-alive.
var nonStreamKeepAliveInterval = 5 * time.Second

// nonStreamKeepAliveSession sends periodic whitespace to keep the client connection alive
// while waiting for an upstream non-streaming response.
type nonStreamKeepAliveSession struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopper sync.Once
}

// startNonStreamKeepAlive starts a keep-alive session for non-streaming requests.
// It writes a single space periodically to prevent client-side timeouts.
func startNonStreamKeepAlive(w http.ResponseWriter) *nonStreamKeepAliveSession {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}

	session := &nonStreamKeepAliveSession{
		writer:  w,
		flusher: flusher,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	go session.heartbeatLoop()
	return session
}

func (s *nonStreamKeepAliveSession) heartbeatLoop() {
	defer close(s.doneCh)

	interval := nonStreamKeepAliveInterval
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if _, ok := safeWrite(s.writer, []byte(" ")); !ok {
				return
			}
			if !safeFlush(s.flusher) {
				return
			}
		}
	}
}

func (s *nonStreamKeepAliveSession) Stop() {
	if s == nil {
		return
	}
	s.stopper.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

func cancelOnClose(body io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	if body == nil {
		return nil
	}
	return &cancelOnCloseReadCloser{ReadCloser: body, cancel: cancel}
}

// isSSE checks if the response is a Server-Sent Events stream.
func isSSE(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/event-stream")
}

func resolveBridgeModel(snap *snapshot.Snapshot, model string, userAgent string) string {
	model = strings.TrimSpace(model)
	if snap == nil || !snap.CompatPolicy.Bridge.Enabled || model == "" {
		return model
	}
	for _, exclude := range snap.CompatPolicy.Bridge.ExcludeUserAgents {
		if wildcardMatch(strings.TrimSpace(exclude), userAgent) {
			return model
		}
	}
	for _, rule := range snap.CompatPolicy.Bridge.Rules {
		if strings.TrimSpace(rule.To) == "" {
			continue
		}
		if wildcardMatch(strings.TrimSpace(rule.From), model) {
			return strings.TrimSpace(rule.To)
		}
	}
	return model
}

func wildcardMatch(pattern string, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return false
	}
	if ok, err := path.Match(pattern, value); err == nil && ok {
		return true
	}
	return strings.EqualFold(pattern, value)
}
