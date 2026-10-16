package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
)

const CorruptionHeader = "X-Agent-Context-Probe-Corruption"
const CorruptionIDHeader = "X-Agent-Context-Probe-Corruption-Id"
const CorruptionCaptureHeader = "X-Agent-Context-Probe-Corruption-Capture"

type responseCorruptor struct {
	cfg           config.ResponseCorruptionConfig
	logger        *log.Logger
	mu            sync.Mutex
	rng           *rand.Rand
	sequence      atomic.Uint64
	eligibleCount int
}

type corruptionRequest struct {
	Model string `json:"model"`
	Tools []struct {
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

// ResponseCorruption runs after Logging has captured the unmodified request.
// Faults are applied to the final client protocol, after provider conversion and
// response capture. A private context cancels upstream work once a fault is sent.
func ResponseCorruption(cfg config.ResponseCorruptionConfig, logger *log.Logger) func(http.Handler) http.Handler {
	if !cfg.Enabled || cfg.EveryNRequests <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	cfg.Modes = append([]string(nil), cfg.Modes...)
	if len(cfg.Modes) == 0 {
		cfg.Modes = []string{"empty_stop", "repetition", "pseudo_tags", "invalid_tool_arguments", "xml_tool_name", "truncate_stream"}
	}
	if cfg.TruncateAfterBytes == 0 {
		cfg.TruncateAfterBytes = 256
	}
	c := &responseCorruptor{cfg: cfg, logger: logger, rng: rand.New(rand.NewSource(cfg.Seed))}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if r.Method != http.MethodPost || (path != "/v1/messages" && path != "/v1/chat/completions" && path != "/v1/responses") {
				next.ServeHTTP(w, r)
				return
			}
			var meta corruptionRequest
			raw, _ := r.Context().Value(model.BodyBytesKey).([]byte)
			_ = json.Unmarshal(raw, &meta)
			tool := ""
			if len(meta.Tools) > 0 {
				tool = meta.Tools[0].Name
				if tool == "" {
					tool = meta.Tools[0].Function.Name
				}
			}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			writer := &corruptingResponseWriter{ResponseWriter: w, owner: c, cancel: cancel, path: path, model: meta.Model, tool: tool}
			next.ServeHTTP(writer, r.WithContext(ctx))
		})
	}
}

func (c *responseCorruptor) choose(stream bool, tool string) string {
	candidates := make([]string, 0, len(c.cfg.Modes))
	for _, mode := range c.cfg.Modes {
		if mode == "truncate_stream" && !stream {
			continue
		}
		if isToolCorruption(mode) && tool == "" {
			continue
		}
		candidates = append(candidates, mode)
	}
	if len(candidates) == 0 {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eligibleCount++
	if c.eligibleCount < c.cfg.EveryNRequests {
		return ""
	}
	c.eligibleCount = 0
	return candidates[c.rng.Intn(len(candidates))]
}

type corruptingResponseWriter struct {
	http.ResponseWriter
	owner                       *responseCorruptor
	cancel                      context.CancelFunc
	path, model, tool, mode, id string
	committed, emitted, stream  bool
}

func (w *corruptingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *corruptingResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.committed {
		return
	}
	w.committed = true
	if status == http.StatusOK {
		w.stream = strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream")
		w.mode = w.owner.choose(w.stream, w.tool)
		if w.mode != "" {
			w.id = fmt.Sprintf("chaos_%d", w.owner.sequence.Add(1))
			for _, key := range []string{"Content-Length", "Content-Encoding", "ETag", "Content-MD5", "Digest"} {
				w.Header().Del(key)
			}
			w.Header().Set(CorruptionHeader, w.mode)
			w.Header().Set(CorruptionIDHeader, w.id)
			w.Header().Set("Cache-Control", "no-store")
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *corruptingResponseWriter) Write(p []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if w.mode == "" {
		return w.ResponseWriter.Write(p)
	}
	if w.emitted || len(p) == 0 {
		return len(p), nil
	}
	w.emitted = true
	var payload []byte
	if w.mode == "truncate_stream" {
		// Stop inside the first application chunk, with no synthetic terminal event.
		n := len(p) / 2
		if n > w.owner.cfg.TruncateAfterBytes {
			n = w.owner.cfg.TruncateAfterBytes
		}
		payload = p[:n]
	} else {
		payload = syntheticResponse(w.path, w.model, w.tool, w.id, w.mode, w.stream)
	}
	// Flush XML tool-name faults per SSE event so the continuation can reach
	// clients incrementally. HTTP/TCP may still coalesce adjacent writes.
	frames := [][]byte{payload}
	if w.mode == "xml_tool_name" && w.stream {
		frames = bytes.SplitAfter(payload, []byte("\n\n"))
	}
	var err error
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		if _, err = w.ResponseWriter.Write(frame); err != nil {
			break
		}
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
	// Cancel only the derived provider context; the downstream response remains
	// writable and all subsequent handler writes are suppressed.
	w.cancel()
	if w.owner.logger != nil {
		w.owner.logger.Printf("response corruption injected: id=%s mode=%s protocol=%s downstream_bytes=%d", w.id, w.mode, w.path, len(payload))
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
func (w *corruptingResponseWriter) Flush() {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// CorruptionHeaders lets capture distinguish provider bytes from the delivered
// fault without storing synthetic content as an actual upstream model response.
func (w *corruptingResponseWriter) CorruptionHeaders() http.Header {
	h := make(http.Header)
	if w.emitted {
		h.Set(CorruptionHeader, w.mode)
		h.Set(CorruptionIDHeader, w.id)
		h.Set(CorruptionCaptureHeader, "upstream-before-injection")
	}
	return h
}
