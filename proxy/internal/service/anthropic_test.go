package service

import (
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func TestAnthropicServiceDefaultTimeout(t *testing.T) {
	const want = 15 * time.Minute

	service, ok := NewAnthropicService(&config.AnthropicConfig{}).(*anthropicService)
	if !ok {
		t.Fatal("NewAnthropicService returned an unexpected implementation")
	}
	if service.client.Timeout != want {
		t.Fatalf("Anthropic service timeout = %s, want %s", service.client.Timeout, want)
	}
}
