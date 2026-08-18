package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/compress"
	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/telemetry"
)

// promptFixtures are the recorded bodies carrying prompts at the length real
// traffic arrives in, ordered as the corpus was recorded. The structural
// fixtures in the same directory cover wire shapes and carry too little text to
// measure, so they are named out rather than globbed in.
var promptFixtures = []string{
	"beginner rambling about a react state bug",
	"formal bug report with stack trace and package manifest",
	"terse expert asking about a postgres plan",
	"long support-agent system prompt with a casual question",
	"six-turn debugging conversation that goes sideways",
	"tool conversation with a large json tool_result",
	"tool conversation with a wall of log output",
	"code review request that is mostly a pasted diff",
	"casual one-liner with a pasted link",
	"dense prose with no code at all",
	"migration planning with a cached repo-context prefix",
	"long meandering devops question with a compose file",
	"four-turn api design discussion with mixed prose and snippets",
}

type corpusEntry struct {
	name    string
	request ir.Request
}

// corpusDir is where the recorded bodies live relative to the repository root.
// Measuring reads them from disk rather than embedding them, so the corpus the
// tests gate against and the corpus the numbers come from cannot drift apart.
const corpusDir = "testdata/golden/fixtures"

func loadCorpus(root string) ([]corpusEntry, error) {
	dir := filepath.Join(root, corpusDir)
	indexBytes, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("reading the corpus index: %w", err)
	}
	var index []struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return nil, fmt.Errorf("parsing the corpus index: %w", err)
	}
	files := map[string]string{}
	for _, entry := range index {
		files[entry.Name] = entry.File
	}

	entries := make([]corpusEntry, 0, len(promptFixtures))
	for _, name := range promptFixtures {
		file, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("the corpus index does not name %q", name)
		}
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		value, err := ir.Unmarshal(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		body, ok := value.(*ir.Object)
		if !ok {
			return nil, fmt.Errorf("%s: the body is not an object", file)
		}
		entries = append(entries, corpusEntry{name: name, request: anthropic.ToIR(body)})
	}
	return entries, nil
}

type measurement struct {
	name         string
	tokensBefore int
	tokensAfter  int
	ratio        float64
	proseShare   float64
}

func measureOne(entry corpusEntry, level compress.Level) measurement {
	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   entry.request,
		Level:     level,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	})
	accounting := telemetry.AccountFor(result.Stats)
	share := 0.0
	if result.Stats.CharsBefore > 0 {
		share = float64(result.Stats.CharsProse) / float64(result.Stats.CharsBefore)
	}
	return measurement{
		name:         entry.name,
		tokensBefore: accounting.TokensBefore,
		tokensAfter:  accounting.TokensAfter,
		ratio:        accounting.Ratio,
		proseShare:   share,
	}
}

func percent(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

// group writes a count with en-US thousands separators, which is how the
// savings log prints its own totals.
func group(value int) string {
	digits := fmt.Sprintf("%d", value)
	if len(digits) <= 3 {
		return digits
	}
	out := strings.Builder{}
	lead := len(digits) % 3
	if lead > 0 {
		out.WriteString(digits[:lead])
	}
	for at := lead; at < len(digits); at += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(digits[at : at+3])
	}
	return out.String()
}

func padRight(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}

func padLeft(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return strings.Repeat(" ", width-len(text)) + text
}

func truncate(text string, width int) string {
	if len(text) <= width {
		return text
	}
	return text[:width]
}

func (c *CLI) printCorpusTotals(corpus []corpusEntry) {
	c.streams.say("corpus")
	for _, level := range compress.LevelNames {
		before, after := 0, 0
		for _, entry := range corpus {
			m := measureOne(entry, level)
			before += m.tokensBefore
			after += m.tokensAfter
		}
		saved := 0.0
		if before > 0 {
			saved = float64(after-before) / float64(before)
		}
		c.streams.say("  %s %s → %s tok  %s",
			padRight(string(level), 9), group(before), group(after), percent(saved))
	}
}

func (c *CLI) printPerRequest(corpus []corpusEntry, level compress.Level) {
	c.streams.say("")
	c.streams.say("by request, at %s levels", level)
	rows := make([]measurement, 0, len(corpus))
	for _, entry := range corpus {
		rows = append(rows, measureOne(entry, level))
	}
	sort.SliceStable(rows, func(left, right int) bool {
		return rows[left].proseShare > rows[right].proseShare
	})
	for _, row := range rows {
		prose := padLeft(fmt.Sprintf("%.0f%%", row.proseShare*100), 4)
		saved := padLeft(percent(-row.ratio), 7)
		c.streams.say("  %s %s prose %s", padRight(truncate(row.name, 48), 48), prose, saved)
	}
}

// Trials per timed subject. A single run measures a cold cache rather than the
// work, so the first few are thrown away and the reported figure is a median.
const (
	warmupRuns = 5
	timedRuns  = 20
)

type timing struct {
	name       string
	median     time.Duration
	low        time.Duration
	high       time.Duration
	charsProse int
}

// timeOne times the pipeline alone. Parsing the wire format is the server's
// cost on every request whether or not compression is on, so ToIR runs once
// during loading and its result is reused; including it would understate the
// share compression adds.
func timeOne(entry corpusEntry, level compress.Level) timing {
	request := compress.PipelineRequest{
		Request:   entry.request,
		Level:     level,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	}
	run := func() (time.Duration, int) {
		started := time.Now()
		result := compress.RunPipeline(request)
		return time.Since(started), result.Stats.CharsProse
	}
	for index := 0; index < warmupRuns; index++ {
		run()
	}
	samples := make([]time.Duration, 0, timedRuns)
	charsProse := 0
	for index := 0; index < timedRuns; index++ {
		elapsed, prose := run()
		samples = append(samples, elapsed)
		charsProse = prose
	}
	sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
	return timing{
		name:       entry.name,
		median:     samples[len(samples)/2],
		low:        samples[0],
		high:       samples[len(samples)-1],
		charsProse: charsProse,
	}
}

func milliseconds(value time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(value.Nanoseconds())/1e6)
}

func (c *CLI) printLevelTimings(corpus []corpusEntry) {
	c.streams.say("corpus, %d runs each after %d warmup", timedRuns, warmupRuns)
	for _, level := range compress.LevelNames {
		total := time.Duration(0)
		charsProse := 0
		for _, entry := range corpus {
			t := timeOne(entry, level)
			total += t.median
			charsProse += t.charsProse
		}
		perSecond := 0.0
		if total > 0 {
			perSecond = float64(charsProse) / total.Seconds()
		}
		c.streams.say("  %s %s  %sk prose chars/s",
			padRight(string(level), 9), padLeft(milliseconds(total), 9),
			group(int(perSecond/1000+0.5)))
	}
}

func (c *CLI) printPerRequestTimings(corpus []corpusEntry, level compress.Level) {
	c.streams.say("")
	c.streams.say("by request, at %s levels", level)
	rows := make([]timing, 0, len(corpus))
	for _, entry := range corpus {
		rows = append(rows, timeOne(entry, level))
	}
	sort.SliceStable(rows, func(left, right int) bool { return rows[left].median > rows[right].median })
	for _, row := range rows {
		spread := milliseconds(row.low) + "–" + milliseconds(row.high)
		c.streams.say("  %s %s %s", padRight(truncate(row.name, 48), 48),
			padLeft(milliseconds(row.median), 9), padLeft(spread, 18))
	}
}

// measure reports what compression saves over the recorded corpus, or how long
// it takes. Savings is the default.
func (c *CLI) measure(argv []string) *exitError {
	mode := "savings"
	for _, argument := range argv {
		switch argument {
		case "--savings", "--performance":
			mode = strings.TrimPrefix(argument, "--")
		default:
			return die(ExitUsage, "caveman: measure takes --savings or --performance, not %q", argument)
		}
	}

	root, err := os.Getwd()
	if err != nil {
		return die(ExitFailure, "caveman: %s", err)
	}
	corpus, err := loadCorpus(root)
	if err != nil {
		return die(ExitFailure, "caveman: %s", err)
	}

	if mode == "performance" {
		c.printLevelTimings(corpus)
		c.printPerRequestTimings(corpus, compress.LevelCaveman)
		return nil
	}
	c.printCorpusTotals(corpus)
	c.printPerRequest(corpus, compress.LevelCaveman)
	return nil
}
