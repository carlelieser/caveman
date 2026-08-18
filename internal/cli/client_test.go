package cli

import "testing"

// The Claude CLI reads ANTHROPIC_CUSTOM_HEADERS as newline-separated
// `Name: Value` lines, so the join has to be a newline and an absent header has
// to drop out rather than leave a blank line behind.
func TestCustomHeadersJoinsWithNewlines(t *testing.T) {
	both := customHeaders(compressHeader("moderate"), countHeader(true))
	want := "X-Caveman-Compress: moderate\nX-Caveman-Count: on"
	if both != want {
		t.Errorf("headers = %q, want %q", both, want)
	}
}

func TestCustomHeadersDropsTheOnesNotSent(t *testing.T) {
	// Counting off sends no count header, and `off` sends no compress header,
	// so neither should contribute a separator.
	if got := customHeaders(compressHeader("moderate"), countHeader(false)); got != "X-Caveman-Compress: moderate" {
		t.Errorf("headers with counting off = %q", got)
	}
	if got := customHeaders(compressHeader(LevelOff), countHeader(true)); got != "X-Caveman-Count: on" {
		t.Errorf("headers with compression off = %q", got)
	}
	if got := customHeaders(compressHeader(LevelOff), countHeader(false)); got != "" {
		t.Errorf("headers with both off = %q", got)
	}
}
