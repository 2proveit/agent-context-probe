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
