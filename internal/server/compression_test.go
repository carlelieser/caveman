package server_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/server"
	"github.com/carlelieser/caveman/internal/telemetry"
)

const verboseSystem = "You are a very helpful assistant that is able to answer all of the questions " +
	"that the user might possibly want to ask you about the current weather."

const compressibleBody = `{"model":"claude-sonnet-4-5","max_tokens":1024,"system":"` + verboseSystem +
	`","messages":[{"role":"user","content":"` + verbosePrompt + `"}]}`

// upperStage stands in for the real pipeline: it honours the policy's level and
// scopes and reports the stats accounting reads, without depending on how words
// are actually dropped.
func upperStage(request ir.Request, p policy.Policy) server.StageResult {
	if !p.CompressionEnabled() {
		return server.StageResult{Request: request}
	}
	scopes := []ir.Scope{}
	for _, scope := range ir.AllScopes {
		if p.Scope[policy.ScopeName(scope)] {
			scopes = append(scopes, scope)
		}
	}
	stats := telemetry.PipelineStats{Level: p.Level}
	for _, node := range ir.CollectTextNodes(request, ir.AllScopes) {
		stats.NodesSeen++
		stats.CharsBefore += len(node.Text)
		stats.CharsProse += len(node.Text)
	}
	compressed := ir.MapTextNodes(request, scopes, func(node ir.TextNode) string {
		stats.NodesCompressed++
		return shorten(node.Text)
	})
	for _, node := range ir.CollectTextNodes(compressed, ir.AllScopes) {
		stats.CharsAfter += len(node.Text)
	}
	return server.StageResult{Request: compressed, Stats: &stats}
}

// shorten drops every other word, which is enough to make the byte count fall.
func shorten(text string) string {
	words := strings.Fields(text)
	kept := []string{}
	for index, word := range words {
		if index%2 == 0 {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

func compressionHandler(t *testing.T, upstream *fakeUpstream) http.Handler {
	t.Helper()
	return serve(t, upstream, server.Options{Stage: upperStage})
}

func TestBodyIsUntouchedWhenCompressionIsOff(t *testing.T) {
	for _, headers := range []map[string]string{nil, {"X-Caveman-Compress": "off"}} {
		upstream := newFakeUpstream(t)
		handler := compressionHandler(t, upstream)

		post(t, handler, "/v1/messages", headers, compressibleBody)

		if got := upstream.Last(t).Body; got != compressibleBody {
			t.Errorf("body changed with compression off\n got %s\nwant %s", got, compressibleBody)
		}
	}
}

func TestCompressionShortensMessageAndSystemText(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := compressionHandler(t, upstream)

	post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "moderate"}, compressibleBody)

	forwarded := upstream.Last(t).Body
	if strings.Contains(forwarded, verbosePrompt) {
		t.Error("message text was not compressed")
	}
	if strings.Contains(forwarded, verboseSystem) {
		t.Error("system text was not compressed under the default scope")
	}
}

// Narrowing the scope must leave everything outside it byte-identical.
func TestScopeNarrowingLeavesSystemAlone(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := compressionHandler(t, upstream)

	post(t, handler, "/v1/messages", map[string]string{
		"X-Caveman-Compress": "moderate",
		"X-Caveman-Scope":    "messages",
	}, compressibleBody)

	forwarded := upstream.Last(t).Body
	if !strings.Contains(forwarded, verboseSystem) {
		t.Error("system text was compressed although the scope named only messages")
	}
	if strings.Contains(forwarded, verbosePrompt) {
		t.Error("message text was not compressed")
	}
}

func TestAccountingHeadersReportTheCompression(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := compressionHandler(t, upstream)

	recorder := post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "moderate"}, compressibleBody)

	before, _ := strconv.Atoi(recorder.Header().Get(telemetry.HeaderTokensBefore))
	after, _ := strconv.Atoi(recorder.Header().Get(telemetry.HeaderTokensAfter))
	if before == 0 || after >= before {
		t.Errorf("tokens before = %d, after = %d", before, after)
	}
	ratio, _ := strconv.ParseFloat(recorder.Header().Get(telemetry.HeaderRatio), 64)
	if ratio <= 0 {
		t.Errorf("ratio = %v", ratio)
	}
	if got := recorder.Header().Get(telemetry.HeaderLevel); got != "moderate" {
		t.Errorf("level header = %q", got)
	}
}

func TestAccountingHeadersAreAbsentWhenCompressionIsOff(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := compressionHandler(t, upstream)

	recorder := post(t, handler, "/v1/messages", nil, compressibleBody)

	for _, name := range []string{telemetry.HeaderRatio, telemetry.HeaderTokensBefore, telemetry.HeaderLevel} {
		if got := recorder.Header().Get(name); got != "" {
			t.Errorf("%s = %q with compression off", name, got)
		}
	}
}

// A compression-induced 4xx stays attributable to the ratio that caused it.
func TestAccountingHeadersSurviveAnUpstreamError(t *testing.T) {
	upstream := newFakeUpstream(t)
	upstream.Reply(func(_ recordedRequest, writer http.ResponseWriter) {
		writer.Header().Set("content-type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"type":"error"}`))
	})
	handler := compressionHandler(t, upstream)

	recorder := post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "moderate"}, compressibleBody)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
	if recorder.Header().Get(telemetry.HeaderRatio) == "" {
		t.Error("accounting headers were dropped on an upstream error")
	}
}

// The compressor reads a node's text and the level, never its position, so the
// same request compresses to the same bytes every time. A body that varied
// would miss the prompt cache on every turn.
func TestCompressingTheSameRequestTwiceSendsIdenticalBytes(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := compressionHandler(t, upstream)

	headers := map[string]string{"X-Caveman-Compress": "moderate"}
	post(t, handler, "/v1/messages", headers, compressibleBody)
	post(t, handler, "/v1/messages", headers, compressibleBody)

	requests := upstream.Requests()
	if len(requests) != 2 {
		t.Fatalf("upstream saw %d requests", len(requests))
	}
	if requests[0].Body != requests[1].Body {
		t.Errorf("the same request compressed to different bytes\nfirst  %s\nsecond %s",
			requests[0].Body, requests[1].Body)
	}
}

// The savings line is written only for a request that was actually compressed.
func TestSavingsAreLoggedOnlyWhenCompressionRan(t *testing.T) {
	upstream := newFakeUpstream(t)
	lines := []string{}
	reporter := telemetry.NewReporter(func(line string) { lines = append(lines, line) })
	handler := serve(t, upstream, server.Options{Stage: upperStage, Reporter: reporter})

	post(t, handler, "/v1/messages", nil, compressibleBody)
	if compressionLines(lines) != 0 {
		t.Errorf("wrote %v with compression off", lines)
	}

	post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "moderate"}, compressibleBody)
	if compressionLines(lines) != 1 {
		t.Errorf("wrote %v for one compressed request", lines)
	}

	post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "0.5"}, compressibleBody)
	if compressionLines(lines) != 1 {
		t.Errorf("a rejected request was logged as compressed: %v", lines)
	}
}

func compressionLines(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.Contains(line, "tok") && !strings.Contains(line, "billed") {
			count++
		}
	}
	return count
}
