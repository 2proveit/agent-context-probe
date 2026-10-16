package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestResponseCorruptionYAML(t *testing.T) {
	clearConfigEnvironment(t)
	standard := isolatedStandardPaths(t)
	cfg, err := LoadWithOptions(LoadOptions{StandardPaths: &standard})
	if err != nil || cfg.ResponseCorruption.Enabled {
		t.Fatalf("must default off: %v", err)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("response_corruption:\n  enabled: true\n  probability: 1\n  seed: 42\n  modes: [empty_stop, truncate_stream, xml_tool_name]\n  truncate_after_bytes: 128\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadWithOptions(LoadOptions{ConfigPath: file, StandardPaths: &standard})
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.ResponseCorruption
	if !c.Enabled || c.Probability != 1 || c.Seed != 42 || len(c.Modes) != 3 || c.Modes[2] != "xml_tool_name" || c.TruncateAfterBytes != 128 {
		t.Fatalf("wrong YAML: %+v", c)
	}
}
func TestResponseCorruptionValidation(t *testing.T) {
	for _, cfg := range []ResponseCorruptionConfig{{Probability: -1}, {Probability: 1.1}, {Probability: math.NaN()}, {Probability: math.Inf(1)}, {Modes: []string{"wrong_type"}}, {TruncateAfterBytes: -1}} {
		if cfg.Validate() == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
	if err := (ResponseCorruptionConfig{}).Validate(); err != nil {
		t.Fatal(err)
	}
}
