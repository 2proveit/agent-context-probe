package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestUIConfigExposesRawDisplayLimits(t *testing.T) {
	h := &Handler{options: Options{
		ShowRawStreamEvents:        true,
		RawRequestMaxDisplayChars:  1200,
		RawResponseMaxDisplayChars: 3400,
	}}
	recorder := httptest.NewRecorder()

	h.UIConfig(recorder, httptest.NewRequest("GET", "/api/ui-config", nil))

	var payload struct {
		ShowRawStreamEvents        bool `json:"showRawStreamEvents"`
		RawRequestMaxDisplayChars  int  `json:"rawRequestMaxDisplayChars"`
		RawResponseMaxDisplayChars int  `json:"rawResponseMaxDisplayChars"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode UI config: %v", err)
	}
	if !payload.ShowRawStreamEvents {
		t.Fatal("expected raw stream events to be enabled")
	}
	if payload.RawRequestMaxDisplayChars != 1200 {
		t.Fatalf("unexpected request display limit: %d", payload.RawRequestMaxDisplayChars)
	}
	if payload.RawResponseMaxDisplayChars != 3400 {
		t.Fatalf("unexpected response display limit: %d", payload.RawResponseMaxDisplayChars)
	}
}
