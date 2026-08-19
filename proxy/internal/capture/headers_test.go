package capture

import (
	"net/http"
	"reflect"
	"testing"
)

func TestHeadersRedactsSensitiveValuesWithoutMutatingSource(t *testing.T) {
	source := http.Header{
		"Authorization":       {"Bearer first", "Bearer second"},
		"Cookie":              {"session=secret"},
		"Set-Cookie":          {"session=response-secret"},
		"X-Api-Key":           {"secret-key"},
		"X-Session-Affinity":  {"session-123"},
		"X-Parent-Session-Id": {"parent-456"},
	}

	got := Headers(source)

	if !reflect.DeepEqual(got.Values("Authorization"), []string{RedactedValue, RedactedValue}) {
		t.Fatalf("authorization values were not redacted: %#v", got.Values("Authorization"))
	}
	for _, name := range []string{"Cookie", "Set-Cookie", "X-Api-Key"} {
		if got.Get(name) != RedactedValue {
			t.Fatalf("%s was not redacted: %q", name, got.Get(name))
		}
	}
	if got.Get("X-Session-Affinity") != "session-123" || got.Get("X-Parent-Session-Id") != "parent-456" {
		t.Fatalf("session headers must remain available: %#v", got)
	}
	if source.Get("Authorization") != "Bearer first" || source.Get("Cookie") != "session=secret" {
		t.Fatalf("source headers were mutated: %#v", source)
	}
}

func TestHeadersMatchesSensitiveNamesCaseInsensitively(t *testing.T) {
	source := http.Header{}
	source["x-aPi-kEy"] = []string{"secret"}
	source["X-Custom-Token"] = []string{"custom-secret"}

	if got := Headers(source)["x-aPi-kEy"]; !reflect.DeepEqual(got, []string{RedactedValue}) {
		t.Fatalf("mixed-case header was not redacted: %#v", got)
	}
	if got := Headers(source)["X-Custom-Token"]; !reflect.DeepEqual(got, []string{RedactedValue}) {
		t.Fatalf("custom token header was not redacted: %#v", got)
	}
}
