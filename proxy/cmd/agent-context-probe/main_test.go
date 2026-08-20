package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "agent-context-probe") || !strings.Contains(stdout.String(), buildinfo.Current().Version) {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestUnknownCommandShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("unexpected error output: %q", stderr.String())
	}
}

func TestHelpFlagsReturnSuccess(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"start", "--help"}, {"doctor", "-h"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("run(%v) exit code = %d, stderr = %q", args, code, stderr.String())
			}
		})
	}
}
