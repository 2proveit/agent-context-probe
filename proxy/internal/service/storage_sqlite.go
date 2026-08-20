package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/seifghazi/claude-code-monitor/internal/capture"
	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
)

type sqliteStorageService struct {
	db     *sql.DB
	config *config.StorageConfig
}

const CurrentSchemaVersion = 2

type MigrationReport struct {
	PreviousVersion int
	CurrentVersion  int
	AppliedVersions []int
	BackupPath      string
}

type SchemaInspection struct {
	Exists            bool
	Version           int
	TargetVersion     int
	MigrationRequired bool
}

type schemaMigration struct {
	version int
	apply   func(*sql.Tx) error
}

var schemaMigrations = []schemaMigration{
	{version: 1, apply: migrateSchemaV1},
	{version: 2, apply: migrateSchemaV2},
}

func NewSQLiteStorageService(cfg *config.StorageConfig) (StorageService, error) {
	storage, _, err := OpenSQLiteStorageService(cfg)
	return storage, err
}

func OpenSQLiteStorageService(cfg *config.StorageConfig) (StorageService, MigrationReport, error) {
	var report MigrationReport
	existed, err := existingDatabaseHasContent(cfg.DBPath)
	if err != nil {
		return nil, report, err
	}
	if err := preparePrivateDatabaseFile(cfg.DBPath); err != nil {
		return nil, report, err
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, report, fmt.Errorf("failed to open database: %w", err)
	}

	service := &sqliteStorageService{
		db:     db,
		config: cfg,
	}

	currentVersion, err := readSchemaVersion(db)
	if err != nil {
		db.Close()
		return nil, report, fmt.Errorf("read schema version: %w", err)
	}
	report.PreviousVersion = currentVersion
	if currentVersion > CurrentSchemaVersion {
		db.Close()
		return nil, report, fmt.Errorf("database schema version %d is newer than supported version %d", currentVersion, CurrentSchemaVersion)
	}

	hasRequests, err := tableExists(db, "requests")
	if err != nil {
		db.Close()
		return nil, report, fmt.Errorf("inspect legacy schema: %w", err)
	}
	// Security normalization intentionally precedes the upgrade backup so a
	// backup never reintroduces persisted credentials from legacy databases.
	if hasRequests {
		if err := service.redactStoredHeaders(); err != nil {
			db.Close()
			return nil, report, fmt.Errorf("redact stored headers before migration: %w", err)
		}
	}
	if existed && currentVersion < CurrentSchemaVersion {
		backupPath, err := createConsistentBackup(db, cfg)
		if err != nil {
			db.Close()
			return nil, report, fmt.Errorf("create pre-migration backup: %w", err)
		}
		report.BackupPath = backupPath
	}

	applied, err := runSchemaMigrations(db, currentVersion, schemaMigrations)
	if err != nil {
		db.Close()
		return nil, report, fmt.Errorf("migrate database schema: %w", err)
	}
	report.AppliedVersions = applied
	report.CurrentVersion = CurrentSchemaVersion
	if err := secureDatabaseArtifacts(cfg.DBPath); err != nil {
		db.Close()
		return nil, report, err
	}

	return service, report, nil
}

func existingDatabaseHasContent(path string) (bool, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return false, nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database file: %w", err)
	}
	return info.Mode().IsRegular() && info.Size() > 0, nil
}

func preparePrivateDatabaseFile(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}

	directory := filepath.Dir(path)
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to inspect database directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close database file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to secure database file: %w", err)
	}
	return nil
}

func secureDatabaseArtifacts(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	for _, candidate := range []string{path, path + "-journal", path + "-shm", path + "-wal"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to secure database artifact %s: %w", candidate, err)
		}
	}
	return nil
}

func migrateSchemaV1(tx *sql.Tx) error {
	schema := `
	CREATE TABLE IF NOT EXISTS requests (
		id TEXT PRIMARY KEY,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		method TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		headers TEXT NOT NULL,
		body TEXT NOT NULL,
		user_agent TEXT,
		content_type TEXT,
		prompt_grade TEXT,
		response TEXT,
		model TEXT,
		original_model TEXT,
		routed_model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON requests(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_endpoint ON requests(endpoint);
	CREATE INDEX IF NOT EXISTS idx_model ON requests(model);
	`

	_, err := tx.Exec(schema)
	return err
}

func migrateSchemaV2(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_original_model ON requests(original_model);
		CREATE INDEX IF NOT EXISTS idx_routed_model ON requests(routed_model);
	`)
	return err
}

func readSchemaVersion(queryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}) (int, error) {
	var version int
	if err := queryer.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func tableExists(queryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}, name string) (bool, error) {
	var count int
	if err := queryer.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func runSchemaMigrations(db *sql.DB, current int, migrations []schemaMigration) ([]int, error) {
	if current == CurrentSchemaVersion {
		return nil, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var applied []int
	for _, migration := range migrations {
		if migration.version <= current {
			continue
		}
		if migration.version > CurrentSchemaVersion {
			break
		}
		if err := migration.apply(tx); err != nil {
			return nil, fmt.Errorf("apply schema version %d: %w", migration.version, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", migration.version)); err != nil {
			return nil, fmt.Errorf("record schema version %d: %w", migration.version, err)
		}
		applied = append(applied, migration.version)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return applied, nil
}

func createConsistentBackup(db *sql.DB, cfg *config.StorageConfig) (string, error) {
	backupDir := cfg.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(cfg.DBPath), "backups")
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("secure backup directory: %w", err)
	}

	currentVersion, err := readSchemaVersion(db)
	if err != nil {
		return "", err
	}
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	base := strings.TrimSuffix(filepath.Base(cfg.DBPath), filepath.Ext(cfg.DBPath))
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-schema-%d-%s.db", base, currentVersion, timestamp))
	for suffix := 1; ; suffix++ {
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		backupPath = filepath.Join(backupDir, fmt.Sprintf("%s-schema-%d-%s-%d.db", base, currentVersion, timestamp, suffix))
	}

	temporaryPath := backupPath + ".tmp"
	defer os.Remove(temporaryPath)
	if _, err := db.Exec("VACUUM INTO ?", temporaryPath); err != nil {
		return "", err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", err
	}
	if err := verifySQLiteDatabase(temporaryPath); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func verifySQLiteDatabase(path string) error {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check returned %q", result)
	}
	return nil
}

func sqliteReadOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro"
}

func InspectSQLiteSchema(path string) (SchemaInspection, error) {
	inspection := SchemaInspection{TargetVersion: CurrentSchemaVersion}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return inspection, nil
	}
	if err != nil {
		return inspection, err
	}
	if !info.Mode().IsRegular() {
		return inspection, fmt.Errorf("database path is not a regular file")
	}
	inspection.Exists = true
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return inspection, err
	}
	defer db.Close()
	inspection.Version, err = readSchemaVersion(db)
	if err != nil {
		return inspection, err
	}
	inspection.MigrationRequired = inspection.Version < CurrentSchemaVersion
	if inspection.Version > CurrentSchemaVersion {
		return inspection, fmt.Errorf("database schema version %d is newer than supported version %d", inspection.Version, CurrentSchemaVersion)
	}
	return inspection, nil
}

func (s *sqliteStorageService) SaveRequest(request *model.RequestLog) (string, error) {
	headersJSON, err := json.Marshal(capture.Headers(request.Headers))
	if err != nil {
		return "", fmt.Errorf("failed to marshal headers: %w", err)
	}

	bodyJSON, err := json.Marshal(request.Body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal body: %w", err)
	}

	query := `
		INSERT INTO requests (id, timestamp, method, endpoint, headers, body, user_agent, content_type, model, original_model, routed_model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		request.RequestID,
		request.Timestamp,
		request.Method,
		request.Endpoint,
		string(headersJSON),
		string(bodyJSON),
		request.UserAgent,
		request.ContentType,
		request.Model,
		request.OriginalModel,
		request.RoutedModel,
	)

	if err != nil {
		return "", fmt.Errorf("failed to insert request: %w", err)
	}

	return request.RequestID, nil
}

func (s *sqliteStorageService) GetRequests(page, limit int) ([]model.RequestLog, int, error) {
	// Get total count
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM requests").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	query := `
		SELECT id, timestamp, method, endpoint, headers, body, model, user_agent, content_type, prompt_grade, response, original_model, routed_model
		FROM requests
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query requests: %w", err)
	}
	defer rows.Close()

	var requests []model.RequestLog
	for rows.Next() {
		var req model.RequestLog
		var headersJSON, bodyJSON string
		var promptGradeJSON, responseJSON sql.NullString

		err := rows.Scan(
			&req.RequestID,
			&req.Timestamp,
			&req.Method,
			&req.Endpoint,
			&headersJSON,
			&bodyJSON,
			&req.Model,
			&req.UserAgent,
			&req.ContentType,
			&promptGradeJSON,
			&responseJSON,
			&req.OriginalModel,
			&req.RoutedModel,
		)
		if err != nil {
			// Error scanning row - skip
			continue
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal([]byte(headersJSON), &req.Headers); err != nil {
			// Error unmarshaling headers
			continue
		}
		req.Headers = capture.Headers(req.Headers)

		var body interface{}
		if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
			// Error unmarshaling body
			continue
		}
		req.Body = body

		if promptGradeJSON.Valid {
			var grade model.PromptGrade
			if err := json.Unmarshal([]byte(promptGradeJSON.String), &grade); err == nil {
				req.PromptGrade = &grade
			}
		}

		if responseJSON.Valid {
			var resp model.ResponseLog
			if err := json.Unmarshal([]byte(responseJSON.String), &resp); err == nil {
				resp.Headers = capture.Headers(resp.Headers)
				req.Response = &resp
			}
		}

		requests = append(requests, req)
	}

	return requests, total, nil
}

func (s *sqliteStorageService) ClearRequests() (int, error) {
	result, err := s.db.Exec("DELETE FROM requests")
	if err != nil {
		return 0, fmt.Errorf("failed to clear requests: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

func (s *sqliteStorageService) UpdateRequestWithGrading(requestID string, grade *model.PromptGrade) error {
	gradeJSON, err := json.Marshal(grade)
	if err != nil {
		return fmt.Errorf("failed to marshal grade: %w", err)
	}

	query := "UPDATE requests SET prompt_grade = ? WHERE id = ?"
	_, err = s.db.Exec(query, string(gradeJSON), requestID)
	if err != nil {
		return fmt.Errorf("failed to update request with grading: %w", err)
	}

	return nil
}

func (s *sqliteStorageService) UpdateRequestWithResponse(request *model.RequestLog) error {
	response := request.Response
	if response != nil {
		copy := *response
		copy.Headers = capture.Headers(response.Headers)
		response = &copy
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	query := "UPDATE requests SET response = ? WHERE id = ?"
	_, err = s.db.Exec(query, string(responseJSON), request.RequestID)
	if err != nil {
		return fmt.Errorf("failed to update request with response: %w", err)
	}

	return nil
}

func (s *sqliteStorageService) EnsureDirectoryExists() error {
	// No directory needed for SQLite
	return nil
}

func (s *sqliteStorageService) GetRequestByShortID(shortID string) (*model.RequestLog, string, error) {
	query := `
		SELECT id, timestamp, method, endpoint, headers, body, model, user_agent, content_type, prompt_grade, response, original_model, routed_model
		FROM requests
		WHERE id LIKE ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var req model.RequestLog
	var headersJSON, bodyJSON string
	var promptGradeJSON, responseJSON sql.NullString

	err := s.db.QueryRow(query, "%"+shortID).Scan(
		&req.RequestID,
		&req.Timestamp,
		&req.Method,
		&req.Endpoint,
		&headersJSON,
		&bodyJSON,
		&req.Model,
		&req.UserAgent,
		&req.ContentType,
		&promptGradeJSON,
		&responseJSON,
		&req.OriginalModel,
		&req.RoutedModel,
	)

	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("request with ID %s not found", shortID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to query request: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal([]byte(headersJSON), &req.Headers); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal headers: %w", err)
	}
	req.Headers = capture.Headers(req.Headers)

	var body interface{}
	if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal body: %w", err)
	}
	req.Body = body

	if promptGradeJSON.Valid {
		var grade model.PromptGrade
		if err := json.Unmarshal([]byte(promptGradeJSON.String), &grade); err == nil {
			req.PromptGrade = &grade
		}
	}

	if responseJSON.Valid {
		var resp model.ResponseLog
		if err := json.Unmarshal([]byte(responseJSON.String), &resp); err == nil {
			resp.Headers = capture.Headers(resp.Headers)
			req.Response = &resp
		}
	}

	return &req, req.RequestID, nil
}

func (s *sqliteStorageService) GetConfig() *config.StorageConfig {
	return s.config
}

func (s *sqliteStorageService) GetAllRequests(modelFilter, headerFilter, sinceFilter string) ([]*model.RequestLog, error) {
	query := `
		SELECT id, timestamp, method, endpoint, headers, body, model, user_agent, content_type, prompt_grade, response, original_model, routed_model
		FROM requests
	`
	args := []interface{}{}
	conditions := []string{}

	if modelFilter != "" && modelFilter != "all" {
		modelPattern := "%" + escapeLike(strings.ToLower(modelFilter)) + "%"
		conditions = append(conditions, `(LOWER(model) LIKE ? ESCAPE '\' OR LOWER(original_model) LIKE ? ESCAPE '\' OR LOWER(routed_model) LIKE ? ESCAPE '\')`)
		args = append(args, modelPattern, modelPattern, modelPattern)
	}

	if headerFilter != "" {
		conditions = append(conditions, `LOWER(headers) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(strings.ToLower(headerFilter))+"%")
	}

	if sinceFilter != "" {
		conditions = append(conditions, "datetime(timestamp) >= datetime(?)")
		args = append(args, sinceFilter)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY timestamp DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query requests: %w", err)
	}
	defer rows.Close()

	var requests []*model.RequestLog
	for rows.Next() {
		var req model.RequestLog
		var headersJSON, bodyJSON string
		var promptGradeJSON, responseJSON sql.NullString

		err := rows.Scan(
			&req.RequestID,
			&req.Timestamp,
			&req.Method,
			&req.Endpoint,
			&headersJSON,
			&bodyJSON,
			&req.Model,
			&req.UserAgent,
			&req.ContentType,
			&promptGradeJSON,
			&responseJSON,
			&req.OriginalModel,
			&req.RoutedModel,
		)
		if err != nil {
			// Error scanning row - skip
			continue
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal([]byte(headersJSON), &req.Headers); err != nil {
			// Error unmarshaling headers
			continue
		}
		req.Headers = capture.Headers(req.Headers)

		var body interface{}
		if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
			// Error unmarshaling body
			continue
		}
		req.Body = body

		if promptGradeJSON.Valid {
			var grade model.PromptGrade
			if err := json.Unmarshal([]byte(promptGradeJSON.String), &grade); err == nil {
				req.PromptGrade = &grade
			}
		}

		if responseJSON.Valid {
			var resp model.ResponseLog
			if err := json.Unmarshal([]byte(responseJSON.String), &resp); err == nil {
				resp.Headers = capture.Headers(resp.Headers)
				req.Response = &resp
			}
		}

		requests = append(requests, &req)
	}

	return requests, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	).Replace(value)
}

func (s *sqliteStorageService) Close() error {
	if err := secureDatabaseArtifacts(s.config.DBPath); err != nil {
		_ = s.db.Close()
		return err
	}
	return s.db.Close()
}

type storedHeaderUpdate struct {
	id       string
	headers  string
	response sql.NullString
}

func (s *sqliteStorageService) redactStoredHeaders() error {
	rows, err := s.db.Query("SELECT id, headers, response FROM requests")
	if err != nil {
		return err
	}

	var updates []storedHeaderUpdate
	for rows.Next() {
		var id, headersJSON string
		var responseJSON sql.NullString
		if err := rows.Scan(&id, &headersJSON, &responseJSON); err != nil {
			rows.Close()
			return err
		}

		redactedHeaders, headersChanged, err := redactHeaderJSON(headersJSON)
		if err != nil {
			rows.Close()
			return fmt.Errorf("request %s headers: %w", id, err)
		}
		redactedResponse, responseChanged, err := redactResponseHeaderJSON(responseJSON)
		if err != nil {
			rows.Close()
			return fmt.Errorf("request %s response headers: %w", id, err)
		}
		if headersChanged || responseChanged {
			updates = append(updates, storedHeaderUpdate{
				id:       id,
				headers:  redactedHeaders,
				response: redactedResponse,
			})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.Exec("UPDATE requests SET headers = ?, response = ? WHERE id = ?", update.headers, update.response, update.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func redactHeaderJSON(raw string) (string, bool, error) {
	var headers http.Header
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return raw, false, err
	}
	redacted := capture.Headers(headers)
	if reflect.DeepEqual(headers, redacted) {
		return raw, false, nil
	}
	encoded, err := json.Marshal(redacted)
	return string(encoded), true, err
}

func redactResponseHeaderJSON(raw sql.NullString) (sql.NullString, bool, error) {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" || strings.TrimSpace(raw.String) == "null" {
		return raw, false, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &fields); err != nil {
		return raw, false, err
	}
	headersRaw, ok := fields["headers"]
	if !ok {
		return raw, false, nil
	}
	redactedHeaders, changed, err := redactHeaderJSON(string(headersRaw))
	if err != nil || !changed {
		return raw, false, err
	}
	fields["headers"] = json.RawMessage(redactedHeaders)
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw, false, err
	}
	return sql.NullString{String: string(encoded), Valid: true}, true, nil
}
