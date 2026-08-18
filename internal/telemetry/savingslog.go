package telemetry

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// The CLI greps these lines, so the separators are part of the contract: two
// spaces between fields, U+2192 between the before and after counts, U+2014
// where a value is missing.
const prefix = "caveman"

const (
	arrow  = "→"
	emDash = "—"
)

var printer = message.NewPrinter(language.AmericanEnglish)

// Reporter tallies one session's compression and what it was billed.
type Reporter struct {
	sink        Sink
	tokensSaved int
	requests    int
}

func NewReporter(sink Sink) *Reporter {
	if sink == nil {
		sink = SilentSink
	}
	return &Reporter{sink: sink}
}

func count(value int) string { return printer.Sprintf("%d", value) }

// percent is the ratio actually achieved, not the one the header asked for.
func percent(ratio float64) string {
	return "-" + strconv.FormatFloat(ratio*100, 'f', 1, 64) + "%"
}

func plural(value int, noun string) string {
	if value == 1 {
		return count(value) + " " + noun
	}
	return count(value) + " " + noun + "s"
}

// nodes reports skipped nodes only when something was skipped. Under the
// default cache mode nothing is, so the count would read `0 cached` on every
// line, spending width on a number that never changes.
func nodes(stats PipelineStats) string {
	fields := []string{plural(stats.NodesSeen, "node")}
	if stats.NodesSkipped > 0 {
		fields = append(fields, count(stats.NodesSkipped)+" cached")
	}
	fields = append(fields, count(stats.NodesCompressed)+" compressed")
	return strings.Join(fields, ", ")
}

// prose reports no share for zero-length input rather than a zero one.
func prose(stats PipelineStats) string {
	if stats.CharsBefore == 0 {
		return emDash
	}
	share := float64(stats.CharsProse) / float64(stats.CharsBefore) * 100
	return strconv.FormatFloat(share, 'f', 0, 64) + "% prose"
}

func requestLine(accounting TokenAccounting, stats PipelineStats, tokensSaved int) string {
	saving := strings.Join([]string{
		fmt.Sprintf("%s %s %s tok", count(accounting.TokensBefore), arrow, count(accounting.TokensAfter)),
		percent(accounting.Ratio),
		string(accounting.Level),
		nodes(stats),
		prose(stats),
	}, "  ")
	return fmt.Sprintf("%s  %s  %s  session %s saved", prefix, saving, emDash, count(tokensSaved))
}

func summaryLine(tokensSaved, requests int) string {
	return fmt.Sprintf("%s  session  %s tok saved across %s",
		prefix, count(tokensSaved), plural(requests, "request"))
}

// billed is a count the response did not carry, rather than a zero it did.
func billed(value *int) string {
	if value == nil {
		return emDash
	}
	return count(*value)
}

// usageLine is what the provider billed, as opposed to what Caveman estimated.
// A cache read means a forwarded prefix still matched; a cache write means it
// was stored fresh.
func usageLine(usage Usage) string {
	fields := strings.Join([]string{
		billed(usage.InputTokens) + " in",
		billed(usage.OutputTokens) + " out",
		billed(usage.CacheReadTokens) + " cache read",
		billed(usage.CacheCreationTokens) + " cache write",
	}, "  ")
	return fmt.Sprintf("%s  billed  %s", prefix, fields)
}

func (r *Reporter) Record(stats PipelineStats) {
	accounting := AccountFor(stats)
	r.tokensSaved += accounting.TokensSaved
	r.requests++
	r.sink(requestLine(accounting, stats, r.tokensSaved))
}

// RecordUsage reports what the provider billed for one request. Separate from
// Record because it arrives later — the counts are in the response, which is
// still streaming when the compression stats are already known.
func (r *Reporter) RecordUsage(usage Usage) {
	if !usage.Any() {
		return
	}
	r.sink(usageLine(usage))
}

// Summary is empty until a request has been compressed, so an idle run stays
// quiet.
func (r *Reporter) Summary() (string, bool) {
	if r.requests == 0 {
		return "", false
	}
	return summaryLine(r.tokensSaved, r.requests), true
}
