package provider

import (
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func TestProviderDefaultTimeouts(t *testing.T) {
	const want = 15 * time.Minute

	openAIProvider := NewOpenAIProvider(&config.OpenAIProviderConfig{})
	if openAIProvider.client.Timeout != want {
		t.Fatalf("OpenAI provider timeout = %s, want %s", openAIProvider.client.Timeout, want)
	}

	anthropicProvider := NewAnthropicProvider(&config.AnthropicProviderConfig{})
	if anthropicProvider.client.Timeout != want {
		t.Fatalf("Anthropic provider timeout = %s, want %s", anthropicProvider.client.Timeout, want)
	}
}
