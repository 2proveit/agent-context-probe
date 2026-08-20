package app

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Server: config.ServerConfig{
			Host:         "127.0.0.1",
			Port:         "3001",
			ReadTimeout:  time.Minute,
			WriteTimeout: time.Minute,
			IdleTimeout:  time.Minute,
		},
		Providers: config.ProvidersConfig{
			Anthropic: config.AnthropicProviderConfig{BaseURL: "https://api.anthropic.com", Version: "2023-06-01"},
			OpenAI:    config.OpenAIProviderConfig{BaseURL: "https://api.openai.com/v1"},
		},
		Storage:   config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")},
		Subagents: config.SubagentsConfig{Mappings: map[string]string{}},
	}
}

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":  &fstest.MapFile{Data: []byte("<html>Agent Context Probe</html>")},
		"favicon.ico": &fstest.MapFile{Data: []byte("icon")},
	}
}

func TestApplicationServesAPIAndDashboardWithoutWildcardCORS(t *testing.T) {
	application, err := New(testConfig(t), log.New(io.Discard, "", 0), Options{Assets: testAssets()})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	defer application.Close()

	health := httptest.NewRecorder()
	application.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}
	if origin := health.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("default response exposes CORS origin %q", origin)
	}

	dashboard := httptest.NewRecorder()
	application.Handler().ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/", nil))
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "Agent Context Probe") {
		t.Fatalf("dashboard response = %d %q", dashboard.Code, dashboard.Body.String())
	}

	missingAPI := httptest.NewRecorder()
	application.Handler().ServeHTTP(missingAPI, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	if missingAPI.Code != http.StatusNotFound || !strings.Contains(missingAPI.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("missing API response = %d %q", missingAPI.Code, missingAPI.Header().Get("Content-Type"))
	}
}

func TestApplicationEnforcesRemoteAccessToken(t *testing.T) {
	cfg := testConfig(t)
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AccessToken = "0123456789abcdef"
	application, err := New(cfg, log.New(io.Discard, "", 0), Options{Assets: testAssets()})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	defer application.Close()

	unauthorized := httptest.NewRecorder()
	application.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/requests", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	authorizedRequest.Header.Set("X-Agent-Context-Probe-Token", cfg.Server.AccessToken)
	authorized := httptest.NewRecorder()
	application.Handler().ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d: %s", authorized.Code, authorized.Body.String())
	}
}
