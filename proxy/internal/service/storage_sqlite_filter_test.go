package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestGetAllRequestsAppliesFilters(t *testing.T) {
	storage, err := NewSQLiteStorageService(&config.StorageConfig{
		DBPath: filepath.Join(t.TempDir(), "requests.db"),
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	now := time.Now()
	requests := []*model.RequestLog{
		{
			RequestID: "older-aime",
			Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339),
			Method:    "POST",
			Endpoint:  "/v1/chat/completions",
			Headers:   map[string][]string{"X-Tenant": {"research"}},
			Body:      map[string]interface{}{"model": "AIME-Atlas"},
			Model:     "AIME-Atlas",
		},
		{
			RequestID: "recent-deepseek",
			Timestamp: now.Add(-30 * time.Minute).Format(time.RFC3339),
			Method:    "POST",
			Endpoint:  "/v1/chat/completions",
			Headers:   map[string][]string{"X-Client-Name": {"Codex Desktop"}},
			Body:      map[string]interface{}{"model": "deepseek-v4-flash"},
			Model:     "deepseek-v4-flash",
		},
		{
			RequestID:   "recent-routed-aime",
			Timestamp:   now.Add(-20 * time.Minute).Format(time.RFC3339),
			Method:      "POST",
			Endpoint:    "/v1/chat/completions",
			Headers:     map[string][]string{"X-Client-Name": {"Mobile"}},
			Body:        map[string]interface{}{"model": "claude-sonnet"},
			Model:       "claude-sonnet",
			RoutedModel: "AIME-Atlas",
		},
	}

	for _, request := range requests {
		if _, err := storage.SaveRequest(request); err != nil {
			t.Fatalf("save request %s: %v", request.RequestID, err)
		}
	}

	tests := []struct {
		name         string
		modelFilter  string
		headerFilter string
		sinceFilter  string
		expectedIDs  []string
	}{
		{
			name:        "model matches case-insensitively across routed model",
			modelFilter: "aime",
			expectedIDs: []string{"recent-routed-aime", "older-aime"},
		},
		{
			name:         "header matches names and values",
			headerFilter: "codex desktop",
			expectedIDs:  []string{"recent-deepseek"},
		},
		{
			name:        "time range compares RFC3339 timestamps",
			sinceFilter: now.Add(-time.Hour).UTC().Format(time.RFC3339),
			expectedIDs: []string{"recent-routed-aime", "recent-deepseek"},
		},
		{
			name:         "filters combine with AND",
			modelFilter:  "AIME",
			headerFilter: "mobile",
			sinceFilter:  now.Add(-time.Hour).UTC().Format(time.RFC3339),
			expectedIDs:  []string{"recent-routed-aime"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := storage.GetAllRequests(test.modelFilter, test.headerFilter, test.sinceFilter)
			if err != nil {
				t.Fatalf("get requests: %v", err)
			}
			if len(results) != len(test.expectedIDs) {
				t.Fatalf("got %d requests, want %d", len(results), len(test.expectedIDs))
			}
			for index, expectedID := range test.expectedIDs {
				if results[index].RequestID != expectedID {
					t.Fatalf("result %d ID = %q, want %q", index, results[index].RequestID, expectedID)
				}
			}
		})
	}
}
