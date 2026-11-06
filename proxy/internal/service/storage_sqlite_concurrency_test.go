package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
	"modernc.org/sqlite"
)

func TestSQLiteConcurrentCaptures(t *testing.T) {
	storage, err := NewSQLiteStorageService(&config.StorageConfig{
		DBPath: filepath.Join(t.TempDir(), "requests.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	db := storage.(*sqliteStorageService).db
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}

	const workers, perWorker = 8, 30
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < perWorker; iteration++ {
				request := sqliteCaptureFixture(fmt.Sprintf("capture-%d-%d", worker, iteration))
				if _, err := storage.SaveRequest(request); err != nil {
					errors <- fmt.Errorf("save %s: %w", request.RequestID, err)
					return
				}
				if err := storage.UpdateRequestWithResponse(request); err != nil {
					errors <- fmt.Errorf("update %s: %w", request.RequestID, err)
					return
				}
				got, _, err := storage.GetRequestByShortID(request.RequestID)
				if err != nil {
					errors <- err
					return
				}
				if !reflect.DeepEqual(got.Response, request.Response) {
					errors <- fmt.Errorf("response mismatch for %s: got %+v", request.RequestID, got.Response)
					return
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	requests, total, err := storage.GetRequests(1, workers*perWorker)
	if err != nil {
		t.Fatal(err)
	}
	if total != workers*perWorker || len(requests) != total {
		t.Errorf("stored %d rows (%d readable), want %d", total, len(requests), workers*perWorker)
	}
	for _, request := range requests {
		if !reflect.DeepEqual(request.Response, sqliteCaptureFixture(request.RequestID).Response) {
			t.Errorf("missing or changed response for %s", request.RequestID)
		}
	}
	var integrity string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("quick_check = %q, error = %v", integrity, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStorageService(storage.GetConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.GetAllRequests("", "", "")
	if err != nil || len(persisted) != workers*perWorker {
		t.Fatalf("reopened captures = %d, error = %v", len(persisted), err)
	}
	for _, request := range persisted {
		if !reflect.DeepEqual(request.Response, sqliteCaptureFixture(request.RequestID).Response) {
			t.Errorf("response changed after reopen for %s", request.RequestID)
		}
	}
}

func TestSQLiteResponseWaitsForExternalWriter(t *testing.T) {
	storage, dbPath := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("wait-for-writer")
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatal(err)
	}
	writer := holdSQLiteWriter(t, dbPath)
	result := make(chan error, 1)
	go func() { result <- storage.UpdateRequestWithResponse(request) }()
	select {
	case err := <-result:
		t.Fatalf("update returned before writer released lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("update did not finish after writer released lock")
	}
	got, _, err := storage.GetRequestByShortID(request.RequestID)
	if err != nil || !reflect.DeepEqual(got.Response, request.Response) {
		t.Fatalf("response was not preserved: got %+v, error = %v", got, err)
	}
}

func TestSQLiteResponseLockTimeout(t *testing.T) {
	storage, dbPath := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("locked-response")
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatal(err)
	}
	holdSQLiteWriter(t, dbPath)
	started := time.Now()
	err := storage.UpdateRequestWithResponse(request)
	elapsed := time.Since(started)
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 5 {
		t.Fatalf("error = %v, want SQLITE_BUSY", err)
	}
	if !strings.Contains(err.Error(), request.RequestID) {
		t.Errorf("error does not identify request: %v", err)
	}
	if elapsed < 2500*time.Millisecond || elapsed > 6*time.Second {
		t.Errorf("lock wait = %v, want approximately 3 seconds", elapsed)
	}
	got, _, err := storage.GetRequestByShortID(request.RequestID)
	if err != nil || got.Response != nil {
		t.Fatalf("failed write changed response: got %+v, error = %v", got, err)
	}
}

func TestSQLiteResponseUpdateRequiresExistingRequest(t *testing.T) {
	storage, _ := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("missing-request")
	err := storage.UpdateRequestWithResponse(request)
	if !errors.Is(err, sql.ErrNoRows) || !strings.Contains(err.Error(), request.RequestID) {
		t.Fatalf("error = %v, want missing request ID and sql.ErrNoRows", err)
	}
}

func TestSQLiteBusyTimeoutSurvivesConnectionReplacement(t *testing.T) {
	storage, dbPath := newSQLiteLockTestStorage(t)
	db := storage.(*sqliteStorageService).db
	// Each query must use a new physical connection, rather than inheriting
	// a PRAGMA set once at startup.
	db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		var timeout int
		if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 3000 {
			t.Fatalf("replacement connection busy_timeout = %d, want 3000", timeout)
		}
	}
	if storage.GetConfig().DBPath != dbPath {
		t.Fatalf("configured path changed to %q", storage.GetConfig().DBPath)
	}
	inspection, err := InspectSQLiteSchema(dbPath)
	if err != nil || inspection.Version != CurrentSchemaVersion {
		t.Fatalf("schema inspection after replacement: %+v, error = %v", inspection, err)
	}
}

func TestSQLiteWALReaderDoesNotBlockCapture(t *testing.T) {
	storage, dbPath := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("active-reader")
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", sqliteReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.Query("SELECT id FROM requests")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reader did not acquire a snapshot: %v", rows.Err())
	}
	result := make(chan error, 1)
	go func() { result <- storage.UpdateRequestWithResponse(request) }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WAL reader blocked response persistence")
	}
}

func TestSQLiteCaptureAfterCloseReturnsRequestID(t *testing.T) {
	storage, _ := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("closed-storage")
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	_, saveErr := storage.SaveRequest(request)
	for _, err := range []error{saveErr, storage.UpdateRequestWithResponse(request)} {
		if err == nil || !strings.Contains(err.Error(), request.RequestID) {
			t.Fatalf("closed storage error = %v, want request ID", err)
		}
	}
}

func TestSQLiteCloseRejectsQueuedCapture(t *testing.T) {
	storage, _ := newSQLiteLockTestStorage(t)
	request := sqliteCaptureFixture("queued-during-close")
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatal(err)
	}
	db := storage.(*sqliteStorageService).db
	connection, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	waits := db.Stats().WaitCount
	result := make(chan error, 1)
	go func() { result <- storage.UpdateRequestWithResponse(request) }()
	deadline := time.Now().Add(time.Second)
	for db.Stats().WaitCount == waits {
		if time.Now().After(deadline) {
			t.Fatal("capture did not queue behind held connection")
		}
		time.Sleep(time.Millisecond)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), request.RequestID) {
			t.Fatalf("queued capture error = %v, want request ID", err)
		}
	case <-time.After(time.Second):
		t.Fatal("closing storage did not unblock queued capture")
	}
}

func newSQLiteLockTestStorage(t *testing.T) (StorageService, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "requests.db")
	storage, err := NewSQLiteStorageService(&config.StorageConfig{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := storage.(*sqliteStorageService).db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}
	return storage, dbPath
}

func holdSQLiteWriter(t *testing.T, path string) *sql.Tx {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	if _, err := tx.Exec("UPDATE requests SET response = response"); err != nil {
		t.Fatal(err)
	}
	return tx
}

func sqliteCaptureFixture(id string) *model.RequestLog {
	return &model.RequestLog{
		RequestID: id,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Method:    "POST",
		Endpoint:  "/v1/chat/completions",
		Headers:   map[string][]string{"Content-Type": {"application/json"}},
		Body:      map[string]interface{}{"model": "test-model", "request": id},
		Model:     "test-model",
		Response: &model.ResponseLog{
			StatusCode:   200,
			Headers:      map[string][]string{"Content-Type": {"application/json"}},
			Body:         json.RawMessage(fmt.Sprintf(`{"id":%q,"choices":[{"finish_reason":"stop"}]}`, id)),
			CompletedAt:  "2026-11-06T00:00:00Z",
			ResponseTime: 42,
		},
	}
}
