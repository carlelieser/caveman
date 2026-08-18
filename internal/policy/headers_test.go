package policy_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/policy"
)

func headersWith(entries map[string]string) http.Header {
	headers := http.Header{}
	for name, value := range entries {
		headers.Set(name, value)
	}
	return headers
}

func mustParse(t *testing.T, entries map[string]string) policy.Policy {
	t.Helper()
	parsed, failure := policy.Parse(headersWith(entries))
	if failure != nil {
		t.Fatalf("unexpected rejection: %v", failure)
	}
	return parsed
}

func mustReject(t *testing.T, entries map[string]string) *policy.Failure {
	t.Helper()
	_, failure := policy.Parse(headersWith(entries))
	if failure == nil {
		t.Fatalf("expected a rejection for %v", entries)
	}
	return failure
}

func scopeEquals(got policy.Scope, messages, system, toolResults bool) bool {
	return got[policy.ScopeMessages] == messages &&
		got[policy.ScopeSystem] == system &&
		got[policy.ScopeToolResults] == toolResults
}

func TestDefaults(t *testing.T) {
	parsed := mustParse(t, map[string]string{})
	if parsed.CompressionEnabled() {
		t.Error("compression defaulted to on")
	}
	if !scopeEquals(parsed.Scope, true, true, true) {
		t.Errorf("scope defaulted to %v", parsed.Scope)
	}
	if parsed.CacheMode != policy.CacheIgnore {
		t.Errorf("cache mode defaulted to %q", parsed.CacheMode)
	}
}

func TestCompressLevels(t *testing.T) {
	for _, level := range []string{"light", "moderate", "caveman"} {
		t.Run(level, func(t *testing.T) {
			parsed := mustParse(t, map[string]string{policy.CompressHeader: level})
			if string(parsed.Level) != level {
				t.Errorf("level = %q, want %q", parsed.Level, level)
			}
		})
	}
}

func TestCompressOffDisablesCompression(t *testing.T) {
	parsed := mustParse(t, map[string]string{policy.CompressHeader: "off"})
	if parsed.CompressionEnabled() {
		t.Error(`"off" left compression enabled`)
	}
}

func TestCompressNormalizesCaseAndWhitespace(t *testing.T) {
	cases := map[string]policy.Level{"CAVEMAN": policy.LevelCaveman, "  light  ": policy.LevelLight}
	for raw, want := range cases {
		parsed := mustParse(t, map[string]string{policy.CompressHeader: raw})
		if parsed.Level != want {
			t.Errorf("%q parsed to %q, want %q", raw, parsed.Level, want)
		}
	}
}

// A fraction used to mean "drop this share of the tokens". Nothing removes a
// share of a class, so a number must be rejected rather than mapped onto a
// level.
func TestCompressRejections(t *testing.T) {
	cases := []string{"0.5", "0", "", "   ", "aggressive", "heuristic"}
	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			failure := mustReject(t, map[string]string{policy.CompressHeader: value})
			if failure.Header != policy.CompressHeader {
				t.Errorf("rejection named %q", failure.Header)
			}
			if failure.Value != value {
				t.Errorf("rejection reported value %q, want %q", failure.Value, value)
			}
		})
	}
}

func TestCompressRejectionListsTheLevels(t *testing.T) {
	failure := mustReject(t, map[string]string{policy.CompressHeader: "0.5"})
	for _, level := range []string{"light", "caveman"} {
		if !strings.Contains(failure.Reason, level) {
			t.Errorf("reason %q does not list %q", failure.Reason, level)
		}
	}
}

func TestScopeParsing(t *testing.T) {
	cases := []struct {
		value                        string
		messages, system, toolResult bool
	}{
		{"system", false, true, false},
		{"messages", true, false, false},
		{"messages,system,tool_results", true, true, true},
		{"messages, system", true, true, false},
	}
	for _, test := range cases {
		t.Run(test.value, func(t *testing.T) {
			parsed := mustParse(t, map[string]string{policy.ScopeHeader: test.value})
			if !scopeEquals(parsed.Scope, test.messages, test.system, test.toolResult) {
				t.Errorf("scope = %v", parsed.Scope)
			}
		})
	}
}

func TestScopeRejections(t *testing.T) {
	for _, value := range []string{"", "messages,", "messages,bogus", "messages,messages"} {
		t.Run(value, func(t *testing.T) {
			failure := mustReject(t, map[string]string{policy.ScopeHeader: value})
			if failure.Header != policy.ScopeHeader {
				t.Errorf("rejection named %q", failure.Header)
			}
		})
	}
}

func TestCacheModes(t *testing.T) {
	for _, mode := range []string{"ignore", "respect"} {
		parsed := mustParse(t, map[string]string{policy.CacheHeader: mode})
		if string(parsed.CacheMode) != mode {
			t.Errorf("%q parsed to %q", mode, parsed.CacheMode)
		}
	}
	parsed := mustParse(t, map[string]string{policy.CacheHeader: " RESPECT "})
	if parsed.CacheMode != policy.CacheRespect {
		t.Errorf("mixed-case mode parsed to %q", parsed.CacheMode)
	}
}

func TestCacheRejections(t *testing.T) {
	failure := mustReject(t, map[string]string{policy.CacheHeader: "maybe"})
	if failure.Header != policy.CacheHeader {
		t.Errorf("rejection named %q", failure.Header)
	}
	if !strings.Contains(failure.Reason, "ignore, respect") {
		t.Errorf("reason %q does not list the modes", failure.Reason)
	}
	if failure := mustReject(t, map[string]string{policy.CacheHeader: "   "}); failure.Header != policy.CacheHeader {
		t.Errorf("empty value rejection named %q", failure.Header)
	}
}

// Every control header Caveman reads must be listed for stripping, or it leaks
// to the provider.
func TestCavemanHeaderNames(t *testing.T) {
	want := []string{"X-Caveman-Compress", "X-Caveman-Scope", "X-Caveman-Cache", "X-Caveman-Count"}
	if len(policy.CavemanHeaderNames) != len(want) {
		t.Fatalf("listed %v", policy.CavemanHeaderNames)
	}
	for i, name := range want {
		if policy.CavemanHeaderNames[i] != name {
			t.Errorf("name %d = %q, want %q", i, policy.CavemanHeaderNames[i], name)
		}
	}
}

// The message is what the client sees, so it must name the header to fix.
func TestFailureMessageNamesTheHeaderAndValue(t *testing.T) {
	failure := mustReject(t, map[string]string{policy.CompressHeader: "bogus"})
	message := failure.Error()
	if !strings.Contains(message, policy.CompressHeader) || !strings.Contains(message, "bogus") {
		t.Errorf("message = %q", message)
	}
}

// Counting costs a tokenizer pass per request, so it happens only when a client
// asks for it by name.
func TestCountDefaultsToOff(t *testing.T) {
	if mustParse(t, nil).Count {
		t.Error("counting is on when no header asked for it")
	}
	if mustParse(t, map[string]string{policy.CountHeader: "off"}).Count {
		t.Error("counting is on for an explicit off")
	}
	if !mustParse(t, map[string]string{policy.CountHeader: "on"}).Count {
		t.Error("counting is off although the header asked for it")
	}
	if !mustParse(t, map[string]string{policy.CountHeader: "  ON  "}).Count {
		t.Error("counting rejected a padded, upper-case on")
	}
}

func TestCountRejectsWhatItCannotRead(t *testing.T) {
	if failure := mustReject(t, map[string]string{policy.CountHeader: "yes"}); failure.Header != policy.CountHeader {
		t.Errorf("rejection named %q", failure.Header)
	}
	if failure := mustReject(t, map[string]string{policy.CountHeader: "   "}); failure.Header != policy.CountHeader {
		t.Errorf("empty value rejection named %q", failure.Header)
	}
}
