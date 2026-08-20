package service

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/capture"
	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestSQLiteStoragePersistsOnlyRedactedHeaders(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "private", "requests.db")
	storage, err := NewSQLiteStorageService(&config.StorageConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	request := &model.RequestLog{
		RequestID: "request-security",
		Timestamp: "2026-08-19T00:00:00Z",
		Method:    "POST",
		Endpoint:  "/v1/messages",
		Headers: map[string][]string{
			"Authorization":      {"Bearer secret"},
			"X-Api-Key":          {"api-secret"},
			"X-Session-Affinity": {"session-visible"},
		},
		Body: map[string]interface{}{"model": "claude-test"},
	}
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatalf("save request: %v", err)
	}
	request.Response = &model.ResponseLog{
		StatusCode: 200,
		Headers: map[string][]string{
			"Set-Cookie": {"response-secret"},
			"X-Trace-Id": {"trace-visible"},
		},
	}
	if err := storage.UpdateRequestWithResponse(request); err != nil {
		t.Fatalf("save response: %v", err)
	}

	got, _, err := storage.GetRequestByShortID("security")
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	if got.Headers["Authorization"][0] != capture.RedactedValue || got.Headers["X-Api-Key"][0] != capture.RedactedValue {
		t.Fatalf("request credentials were persisted: %#v", got.Headers)
	}
	if got.Headers["X-Session-Affinity"][0] != "session-visible" {
		t.Fatalf("session header was removed: %#v", got.Headers)
	}
	if got.Response.Headers["Set-Cookie"][0] != capture.RedactedValue || got.Response.Headers["X-Trace-Id"][0] != "trace-visible" {
		t.Fatalf("response headers were not sanitized correctly: %#v", got.Response.Headers)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("stat database: %v", err)
		}
		if gotMode := info.Mode().Perm(); gotMode != 0o600 {
			t.Fatalf("database mode = %o, want 600", gotMode)
		}
	}
}

func TestSQLiteStorageRedactsExistingRowsOnOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "requests.db")
	storage, err := NewSQLiteStorageService(&config.StorageConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("initialize storage: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close initial storage: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	headers, _ := json.Marshal(map[string][]string{
		"Authorization": {"Bearer historical-secret"},
		"X-Trace-Id":    {"trace-visible"},
	})
	response, _ := json.Marshal(map[string]interface{}{
		"statusCode": 200,
		"headers": map[string][]string{
			"Set-Cookie": {"historical-cookie"},
		},
		"futureField": map[string]interface{}{"kept": true},
	})
	_, err = db.Exec(`INSERT INTO requests (id, timestamp, method, endpoint, headers, body, response, model, original_model, routed_model, user_agent, content_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"historical", "2026-08-19T00:00:00Z", "POST", "/v1/messages", string(headers), `{}`, string(response), "claude-test", "claude-test", "claude-test", "test", "application/json")
	if err != nil {
		db.Close()
		t.Fatalf("seed historical request: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}

	storage, err = NewSQLiteStorageService(&config.StorageConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	defer storage.Close()

	got, _, err := storage.GetRequestByShortID("historical")
	if err != nil {
		t.Fatalf("read migrated request: %v", err)
	}
	if got.Headers["Authorization"][0] != capture.RedactedValue || got.Response.Headers["Set-Cookie"][0] != capture.RedactedValue {
		t.Fatalf("historical credentials were not redacted")
	}

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen raw database: %v", err)
	}
	defer rawDB.Close()
	var storedResponse string
	if err := rawDB.QueryRow("SELECT response FROM requests WHERE id = ?", "historical").Scan(&storedResponse); err != nil {
		t.Fatalf("read raw response: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(storedResponse), &fields); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := fields["futureField"]; !ok {
		t.Fatal("header migration removed an unknown response field")
	}
}
