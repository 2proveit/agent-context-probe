package webui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

type Handler struct {
	assets fs.FS
	index  []byte
}

func NewHandler(assets fs.FS) (*Handler, error) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read dashboard index: %w", err)
	}
	return &Handler{assets: assets, index: index}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.Contains(r.URL.EscapedPath(), "..") {
		writeNotFound(w)
		return
	}

	requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if requested == "." || requested == "" || requested == "ui" {
		h.serveIndex(w, r)
		return
	}

	if info, err := fs.Stat(h.assets, requested); err == nil && !info.IsDir() {
		contents, err := fs.ReadFile(h.assets, requested)
		if err != nil {
			http.Error(w, "failed to read dashboard asset", http.StatusInternalServerError)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(requested))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if strings.HasPrefix(requested, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		http.ServeContent(w, r, requested, time.Time{}, strings.NewReader(string(contents)))
		return
	}

	if path.Ext(requested) != "" {
		writeNotFound(w)
		return
	}
	h.serveIndex(w, r)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(h.index)))
}

func setSecurityHeaders(headers http.Header) {
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("X-Frame-Options", "DENY")
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}
