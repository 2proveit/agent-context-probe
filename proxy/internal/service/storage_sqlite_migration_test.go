package service

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/capture"
	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func TestNewDatabaseMigratesWithoutBackup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Agent Context Probe")
	cfg := &config.StorageConfig{
		DBPath:    filepath.Join(root, "requests.db"),
		BackupDir: filepath.Join(root, "backups"),
	}
	storage, report, err := OpenSQLiteStorageService(cfg)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer storage.Close()

	if report.PreviousVersion != 0 || report.CurrentVersion != CurrentSchemaVersion {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !reflect.DeepEqual(report.AppliedVersions, []int{1, 2}) {
		t.Fatalf("applied versions = %#v", report.AppliedVersions)
	}
	if report.BackupPath != "" {
		t.Fatalf("new database created a backup: %s", report.BackupPath)
	}
	inspection, err := InspectSQLiteSchema(cfg.DBPath)
	if err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if inspection.Version != CurrentSchemaVersion || inspection.MigrationRequired {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
}

func TestLegacyDatabaseIsSanitizedBackedUpAndMigratedOnce(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "requests.db")
	backupDir := filepath.Join(root, "backups")
	seedLegacyDatabase(t, dbPath)

	cfg := &config.StorageConfig{DBPath: dbPath, BackupDir: backupDir}
	storage, report, err := OpenSQLiteStorageService(cfg)
	if err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	if report.BackupPath == "" {
		t.Fatal("legacy database did not create a backup")
	}
	if _, err := os.Stat(report.BackupPath); err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(report.BackupPath); info.Mode().Perm() != 0o600 {
			t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
		}
		if info, _ := os.Stat(backupDir); info.Mode().Perm() != 0o700 {
			t.Fatalf("backup directory mode = %o, want 700", info.Mode().Perm())
		}
	}
	assertDatabaseSecurityAndVersion(t, dbPath, CurrentSchemaVersion)
	assertDatabaseSecurityAndVersion(t, report.BackupPath, 0)

	storage, secondReport, err := OpenSQLiteStorageService(cfg)
	if err != nil {
		t.Fatalf("reopen migrated storage: %v", err)
	}
	defer storage.Close()
	if secondReport.BackupPath != "" || len(secondReport.AppliedVersions) != 0 {
		t.Fatalf("second open repeated migration: %+v", secondReport)
	}
}

func TestFutureSchemaVersionIsRejectedWithoutBackup(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "requests.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	_, _, err = OpenSQLiteStorageService(&config.StorageConfig{
		DBPath:    dbPath,
		BackupDir: filepath.Join(root, "backups"),
	})
	if err == nil {
		t.Fatal("future schema version must be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(root, "backups")); !os.IsNotExist(statErr) {
		t.Fatalf("future version unexpectedly created a backup: %v", statErr)
	}
}

func TestSchemaMigrationsRollbackAsOneTransaction(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	_, err = runSchemaMigrations(db, 0, []schemaMigration{
		{version: 1, apply: func(tx *sql.Tx) error {
			_, err := tx.Exec("CREATE TABLE transient_value (value TEXT)")
			return err
		}},
		{version: 2, apply: func(*sql.Tx) error { return errors.New("injected migration failure") }},
	})
	if err == nil {
		t.Fatal("injected migration failure must be returned")
	}
	exists, err := tableExists(db, "transient_value")
	if err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
	if exists {
		t.Fatal("failed migration left a table behind")
	}
	version, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 0 {
		t.Fatalf("schema version after rollback = %d", version)
	}
}

func seedLegacyDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin legacy schema: %v", err)
	}
	if err := migrateSchemaV1(tx); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO requests (id, timestamp, method, endpoint, headers, body, response, model, original_model, routed_model, user_agent, content_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy", "2026-08-19T00:00:00Z", "POST", "/v1/messages",
		`{"Authorization":["Bearer legacy-secret"],"X-Trace-Id":["trace"]}`,
		`{}`,
		`{"statusCode":200,"headers":{"Set-Cookie":["legacy-cookie"]}}`,
		"claude-test", "claude-test", "claude-test", "test", "application/json")
	if err != nil {
		t.Fatalf("insert legacy request: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}
}

func assertDatabaseSecurityAndVersion(t *testing.T, path string, wantVersion int) {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("open database %s: %v", path, err)
	}
	defer db.Close()
	version, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != wantVersion {
		t.Fatalf("schema version for %s = %d, want %d", path, version, wantVersion)
	}
	var headers, response string
	if err := db.QueryRow("SELECT headers, response FROM requests WHERE id = 'legacy'").Scan(&headers, &response); err != nil {
		t.Fatalf("read legacy request: %v", err)
	}
	if headers == "" || response == "" {
		t.Fatal("legacy request data is missing")
	}
	if containsSensitiveTestValue(headers) || containsSensitiveTestValue(response) {
		t.Fatalf("database %s retained a sensitive test value", path)
	}
	if !containsRedaction(headers) || !containsRedaction(response) {
		t.Fatalf("database %s does not contain redaction markers", path)
	}
}

func containsSensitiveTestValue(value string) bool {
	return strings.Contains(value, "legacy-secret") || strings.Contains(value, "legacy-cookie")
}

func containsRedaction(value string) bool {
	return strings.Contains(value, capture.RedactedValue)
}
