package buildinfo

import "testing"

func TestCurrentReturnsInjectedVariables(t *testing.T) {
	original := Current()
	t.Cleanup(func() {
		Version = original.Version
		Commit = original.Commit
		BuildTime = original.BuildTime
	})
	Version = "1.2.3"
	Commit = "abc123"
	BuildTime = "2026-08-19T00:00:00Z"

	got := Current()
	if got.Version != Version || got.Commit != Commit || got.BuildTime != BuildTime {
		t.Fatalf("unexpected build info: %+v", got)
	}
}
