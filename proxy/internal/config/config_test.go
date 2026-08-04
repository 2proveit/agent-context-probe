package config

import "testing"

func TestConfigRejectsNegativeCaptureLimit(t *testing.T) {
	cfg := &Config{Storage: StorageConfig{MaxCaptureBytes: -1}}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected a negative capture limit to fail validation")
	}
}

func TestConfigAcceptsUnlimitedCapture(t *testing.T) {
	cfg := &Config{Storage: StorageConfig{MaxCaptureBytes: 0}}
	if err := cfg.validate(); err != nil {
		t.Fatalf("zero should enable unlimited capture: %v", err)
	}
}

func TestConfigRejectsNegativeRawDisplayLimits(t *testing.T) {
	tests := []struct {
		name string
		web  WebConfig
	}{
		{
			name: "request",
			web:  WebConfig{RawRequestMaxDisplayChars: -1},
		},
		{
			name: "response",
			web:  WebConfig{RawResponseMaxDisplayChars: -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Web: tt.web}
			if err := cfg.validate(); err == nil {
				t.Fatal("expected a negative raw display limit to fail validation")
			}
		})
	}
}

func TestConfigAcceptsUnlimitedRawDisplay(t *testing.T) {
	cfg := &Config{Web: WebConfig{
		RawRequestMaxDisplayChars:  0,
		RawResponseMaxDisplayChars: 0,
	}}
	if err := cfg.validate(); err != nil {
		t.Fatalf("zero should enable unlimited raw display: %v", err)
	}
}
