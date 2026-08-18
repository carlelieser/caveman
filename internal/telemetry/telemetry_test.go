package telemetry_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/telemetry"
)

func statsFor(charsBefore, charsAfter int) telemetry.PipelineStats {
	return telemetry.PipelineStats{
		Level:           policy.LevelModerate,
		NodesSeen:       1,
		NodesCompressed: 1,
		CharsBefore:     charsBefore,
		CharsAfter:      charsAfter,
		CharsProse:      charsBefore,
	}
}

func TestTokenAccounting(t *testing.T) {
	accounting := telemetry.AccountFor(statsFor(400, 240))
	if accounting.TokensBefore != 100 || accounting.TokensAfter != 60 || accounting.TokensSaved != 40 {
		t.Errorf("accounting = %+v", accounting)
	}
	if got := telemetry.AccountFor(statsFor(400, 400)).TokensSaved; got != 0 {
		t.Errorf("tokens saved with nothing dropped = %d", got)
	}
	empty := telemetry.AccountFor(statsFor(0, 0))
	if empty.TokensSaved != 0 || empty.Ratio != 0 {
		t.Errorf("empty request accounting = %+v", empty)
	}
}

// The estimate rounds up, so a partial token still costs one.
func TestTokenEstimateRoundsUp(t *testing.T) {
	if got := telemetry.AccountFor(statsFor(5, 0)).TokensBefore; got != 2 {
		t.Errorf("5 chars estimated as %d tokens, want 2", got)
	}
}

func TestAccountingHeaders(t *testing.T) {
	headers := http.Header{}
	telemetry.ApplyAccountingHeaders(headers, telemetry.AccountFor(statsFor(400, 240)))
	cases := map[string]string{
		telemetry.HeaderTokensBefore: "100",
		telemetry.HeaderTokensAfter:  "60",
		telemetry.HeaderRatio:        "0.4000",
		telemetry.HeaderLevel:        "moderate",
	}
	for name, want := range cases {
		if got := headers.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func sseEvent(t *testing.T, payload any, name string) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encoding event: %v", err)
	}
	return "event: " + name + "\ndata: " + string(encoded) + "\n\n"
}

func observe(chunks []string) telemetry.Usage {
	observer := telemetry.NewUsageObserver()
	for _, chunk := range chunks {
		observer.Push(chunk)
	}
	return observer.Current()
}

func messageStart(t *testing.T) string {
	return sseEvent(t, map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"type": "message",
			"usage": map[string]any{
				"input_tokens":                5710,
				"output_tokens":               1,
				"cache_read_input_tokens":     4200,
				"cache_creation_input_tokens": 300,
			},
		},
	}, "message_start")
}

func messageDelta(t *testing.T) string {
	return sseEvent(t, map[string]any{
		"type":  "message_delta",
		"usage": map[string]any{"output_tokens": 412},
	}, "message_delta")
}

func wantCount(t *testing.T, got *int, want int, label string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s was absent, want %d", label, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", label, *got, want)
	}
}

func TestUsageFromAStreamedResponse(t *testing.T) {
	usage := observe([]string{messageStart(t)})
	wantCount(t, usage.InputTokens, 5710, "input")
	wantCount(t, usage.CacheReadTokens, 4200, "cache read")
	wantCount(t, usage.CacheCreationTokens, 300, "cache write")
}

// message_delta carries the final output count and nothing else, so it must not
// erase the input counts message_start already reported.
func TestUsageMergesDeltaWithoutErasingEarlierCounts(t *testing.T) {
	usage := observe([]string{messageStart(t), messageDelta(t)})
	wantCount(t, usage.OutputTokens, 412, "output")
	wantCount(t, usage.InputTokens, 5710, "input")
	wantCount(t, usage.CacheReadTokens, 4200, "cache read")
}

// A chunk boundary can fall anywhere, so a partial line is held back until its
// newline arrives.
func TestUsageReadsEventsSplitAcrossChunks(t *testing.T) {
	whole := messageStart(t)
	for _, cut := range []int{1, 7, 20, len(whole) - 3} {
		usage := observe([]string{whole[:cut], whole[cut:]})
		wantCount(t, usage.InputTokens, 5710, "input")
	}
}

func TestUsageReadsAnEventSplitOneByteAtATime(t *testing.T) {
	chunks := []string{}
	for _, char := range messageStart(t) {
		chunks = append(chunks, string(char))
	}
	wantCount(t, observe(chunks).CacheReadTokens, 4200, "cache read")
}

func TestUsageIgnoresEventsWithoutCounts(t *testing.T) {
	usage := observe([]string{
		sseEvent(t, map[string]any{"type": "content_block_delta", "index": 0}, "content_block_delta"),
		sseEvent(t, map[string]any{"type": "ping"}, "ping"),
	})
	if usage.Any() {
		t.Errorf("reported usage %+v for events carrying none", usage)
	}
}

func TestUsageSurvivesANonJSONDataLine(t *testing.T) {
	usage := observe([]string{"data: not json at all\n\n", messageStart(t)})
	wantCount(t, usage.InputTokens, 5710, "input")
}

func TestUsageFromANonStreamedBody(t *testing.T) {
	body := `{"type":"message","usage":{"input_tokens":120,"output_tokens":40,"cache_read_input_tokens":0,"cache_creation_input_tokens":90}}`
	usage := observe([]string{body})
	wantCount(t, usage.InputTokens, 120, "input")
	wantCount(t, usage.OutputTokens, 40, "output")
	wantCount(t, usage.CacheReadTokens, 0, "cache read")
	wantCount(t, usage.CacheCreationTokens, 90, "cache write")
}

func TestUsageReadsABodyDeliveredInChunks(t *testing.T) {
	body := `{"usage":{"input_tokens":7}}`
	middle := len(body) / 2
	wantCount(t, observe([]string{body[:middle], body[middle:]}).InputTokens, 7, "input")
}

func TestUsageReportsNothingForUnreadableBodies(t *testing.T) {
	if observe([]string{"<html>error</html>"}).Any() {
		t.Error("reported usage for a non-JSON body")
	}
	if observe(nil).Any() {
		t.Error("reported usage for an empty body")
	}
	if telemetry.EmptyUsage().Any() {
		t.Error("EmptyUsage reported usage")
	}
}

// A zero the response carried is a real count; an absent field is not. The
// billed line prints them differently, so they must stay distinguishable.
func TestUsageDistinguishesZeroFromAbsent(t *testing.T) {
	usage := observe([]string{`{"usage":{"cache_read_input_tokens":0}}`})
	wantCount(t, usage.CacheReadTokens, 0, "cache read")
	if usage.InputTokens != nil {
		t.Errorf("absent input reported as %d", *usage.InputTokens)
	}
}

func TestEventParserHoldsBackAPartialLine(t *testing.T) {
	parser := &telemetry.EventParser{}
	if got := parser.Push(`data: {"a":1}`); len(got) != 0 {
		t.Errorf("emitted %v before the newline arrived", got)
	}
	got := parser.Push("\n")
	if len(got) != 1 || got[0] != `{"a":1}` {
		t.Errorf("emitted %v after the newline", got)
	}
}

func TestEventParserReadsSeveralPayloadsFromOneChunk(t *testing.T) {
	parser := &telemetry.EventParser{}
	got := parser.Push("data: 1\ndata: 2\n")
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("emitted %v", got)
	}
}

func TestEventParserSkipsNonDataLines(t *testing.T) {
	parser := &telemetry.EventParser{}
	if got := parser.Push("event: message_start\n: comment\n\n"); len(got) != 0 {
		t.Errorf("emitted %v for lines carrying no data", got)
	}
}

func collectLines() (*telemetry.Reporter, *[]string) {
	lines := &[]string{}
	reporter := telemetry.NewReporter(func(line string) { *lines = append(*lines, line) })
	return reporter, lines
}

// The CLI greps these lines, so the separators are part of the contract.
func TestRequestLineFormat(t *testing.T) {
	reporter, lines := collectLines()
	reporter.Record(statsFor(400, 240))
	if len(*lines) != 1 {
		t.Fatalf("wrote %d lines", len(*lines))
	}
	line := (*lines)[0]
	for _, want := range []string{"caveman  ", "100 → 60 tok", "-40.0%", "moderate", "1 node, 1 compressed", "—", "session 40 saved"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not contain %q", line, want)
		}
	}
}

func TestThousandsAreGrouped(t *testing.T) {
	reporter, lines := collectLines()
	reporter.Record(statsFor(40000, 24000))
	if !strings.Contains((*lines)[0], "10,000 → 6,000 tok") {
		t.Errorf("line %q does not group thousands", (*lines)[0])
	}
}

// Skipped nodes earn a column only when something was skipped; otherwise the
// line would read `0 cached` on every request.
func TestSkippedNodesAppearOnlyWhenNonZero(t *testing.T) {
	reporter, lines := collectLines()
	reporter.Record(statsFor(400, 240))
	if strings.Contains((*lines)[0], "cached") {
		t.Errorf("line %q mentions cached nodes when none were skipped", (*lines)[0])
	}

	reporter, lines = collectLines()
	stats := statsFor(400, 400)
	stats.NodesSeen = 2
	stats.NodesSkipped = 2
	stats.NodesCompressed = 0
	reporter.Record(stats)
	if !strings.Contains((*lines)[0], "2 nodes, 2 cached, 0 compressed") {
		t.Errorf("line %q does not name the skipped nodes", (*lines)[0])
	}
}

// Zero-length input has no prose share to report rather than a zero one.
func TestProseShareIsADashForEmptyInput(t *testing.T) {
	reporter, lines := collectLines()
	reporter.Record(statsFor(0, 0))
	if !strings.Contains((*lines)[0], "—") {
		t.Errorf("line %q does not use an em dash for absent prose", (*lines)[0])
	}
}

func TestSessionTotalAccumulates(t *testing.T) {
	reporter, lines := collectLines()
	reporter.Record(statsFor(400, 240))
	reporter.Record(statsFor(400, 240))
	pattern := regexp.MustCompile(`session ([\d,]+) saved`)
	first := pattern.FindStringSubmatch((*lines)[0])
	second := pattern.FindStringSubmatch((*lines)[1])
	if first == nil || second == nil {
		t.Fatalf("lines did not report a session total: %v", *lines)
	}
	if first[1] != "40" || second[1] != "80" {
		t.Errorf("totals were %q then %q, want 40 then 80", first[1], second[1])
	}
}

func TestSummaryIsAbsentUntilSomethingIsCompressed(t *testing.T) {
	reporter, _ := collectLines()
	if _, ok := reporter.Summary(); ok {
		t.Error("an idle reporter produced a summary")
	}
	reporter.Record(statsFor(400, 240))
	summary, ok := reporter.Summary()
	if !ok {
		t.Fatal("no summary after a compressed request")
	}
	if !strings.Contains(summary, "1 request") || strings.Contains(summary, "1 requests") {
		t.Errorf("summary %q does not say 1 request", summary)
	}
	reporter.Record(statsFor(400, 240))
	summary, _ = reporter.Summary()
	if !strings.Contains(summary, "2 requests") || !strings.Contains(summary, "80 tok saved") {
		t.Errorf("summary = %q", summary)
	}
}

func TestBilledLineNamesEveryCount(t *testing.T) {
	reporter, lines := collectLines()
	input, output, cacheRead := 5710, 412, 4200
	reporter.RecordUsage(telemetry.Usage{
		InputTokens: &input, OutputTokens: &output, CacheReadTokens: &cacheRead,
	})
	if len(*lines) != 1 {
		t.Fatalf("wrote %d lines", len(*lines))
	}
	line := (*lines)[0]
	for _, want := range []string{"caveman  billed  ", "5,710 in", "412 out", "4,200 cache read", "— cache write"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not contain %q", line, want)
		}
	}
}

func TestBilledLineIsSkippedWhenNothingWasBilled(t *testing.T) {
	reporter, lines := collectLines()
	reporter.RecordUsage(telemetry.EmptyUsage())
	if len(*lines) != 0 {
		t.Errorf("wrote %v for a response carrying no counts", *lines)
	}
}

func TestLoggingSwitch(t *testing.T) {
	cases := []struct {
		value   string
		present bool
		want    bool
	}{
		{"", false, true},
		{"", true, true},
		{"0", true, true},
		{"false", true, true},
		{"1", true, false},
		{"true", true, false},
	}
	for _, test := range cases {
		// t.Setenv registers the restore; unsetting after it exercises the
		// absent case without leaking the variable into other tests.
		t.Setenv("DISABLE_LOGS", test.value)
		if !test.present {
			if err := os.Unsetenv("DISABLE_LOGS"); err != nil {
				t.Fatalf("unsetting DISABLE_LOGS: %v", err)
			}
		}
		if got := telemetry.LoggingEnabled(); got != test.want {
			t.Errorf("DISABLE_LOGS=%q present=%v: enabled = %v, want %v", test.value, test.present, got, test.want)
		}
	}
}

func TestSinkHonoursTheSwitch(t *testing.T) {
	var written strings.Builder
	sink := telemetry.NewSinkTo(&written)

	t.Setenv("DISABLE_LOGS", "1")
	sink("suppressed")
	if written.Len() != 0 {
		t.Errorf("wrote %q while disabled", written.String())
	}

	t.Setenv("DISABLE_LOGS", "0")
	sink("emitted")
	if written.String() != "emitted\n" {
		t.Errorf("wrote %q, want %q", written.String(), "emitted\n")
	}
}
