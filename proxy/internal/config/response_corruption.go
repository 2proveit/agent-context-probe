package config

import (
	"fmt"
	"math"
)

// ResponseCorruptionConfig controls faults delivered to downstream harnesses.
// It never changes requests sent to model providers.
type ResponseCorruptionConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Probability        float64  `yaml:"probability"`
	Seed               int64    `yaml:"seed"`
	Modes              []string `yaml:"modes"`
	TruncateAfterBytes int      `yaml:"truncate_after_bytes"`
}

func (c ResponseCorruptionConfig) Validate() error {
	if math.IsNaN(c.Probability) || math.IsInf(c.Probability, 0) || c.Probability < 0 || c.Probability > 1 {
		return fmt.Errorf("response_corruption.probability must be finite and between 0 and 1")
	}
	if c.TruncateAfterBytes < 0 {
		return fmt.Errorf("response_corruption.truncate_after_bytes must be zero or greater")
	}
	for _, mode := range c.Modes {
		switch mode {
		case "empty_stop", "repetition", "pseudo_tags", "invalid_tool_arguments", "truncate_stream":
		default:
			return fmt.Errorf("unknown response_corruption mode %q", mode)
		}
	}
	return nil
}
