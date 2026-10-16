package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ResponseCorruptionConfig controls faults delivered to downstream harnesses.
// It never changes requests sent to model providers.
type ResponseCorruptionConfig struct {
	Enabled            bool     `yaml:"enabled"`
	EveryNRequests     int      `yaml:"every_n_requests"`
	Seed               int64    `yaml:"seed"`
	Modes              []string `yaml:"modes"`
	TruncateAfterBytes int      `yaml:"truncate_after_bytes"`
}

func (c ResponseCorruptionConfig) Validate() error {
	if c.EveryNRequests < 0 || (c.Enabled && c.EveryNRequests == 0) {
		return fmt.Errorf("response_corruption.every_n_requests must be greater than zero when enabled, and never negative")
	}
	if c.TruncateAfterBytes < 0 {
		return fmt.Errorf("response_corruption.truncate_after_bytes must be zero or greater")
	}
	for _, mode := range c.Modes {
		switch mode {
		case "empty_stop", "repetition", "pseudo_tags", "invalid_tool_arguments", "xml_tool_name", "truncate_stream":
		default:
			return fmt.Errorf("unknown response_corruption mode %q", mode)
		}
	}
	return nil
}

// YAML otherwise truncates floating-point numbers assigned to int fields.
// Reject them so the configured request interval is never silently changed.
func (c *ResponseCorruptionConfig) UnmarshalYAML(node *yaml.Node) error {
	var fields map[string]yaml.Node
	if err := node.Decode(&fields); err != nil {
		return err
	}
	if interval, ok := fields["every_n_requests"]; ok && interval.Tag != "!!int" {
		return fmt.Errorf("response_corruption.every_n_requests must be an integer")
	}
	type plain ResponseCorruptionConfig
	return node.Decode((*plain)(c))
}
