package compress

import (
	"regexp"
	"sort"
	"strings"
)

const (
	RegionProse     = "prose"
	RegionProtected = "protected"
)

type span struct {
	start int
	end   int
}

// line is a line of the source plus its offset, so line-oriented detection can
// report absolute positions without re-scanning.
type line struct {
	start int
	end   int
	text  string
}

// splitLines cuts on `\n`, keeping the terminator with the line it ends, which
// is what makes the line spans tile the source exactly.
func splitLines(text string) []line {
	lines := []line{}
	start := 0
	for start < len(text) {
		end := strings.IndexByte(text[start:], '\n')
		if end == -1 {
			end = len(text)
		} else {
			end = start + end + 1
		}
		lines = append(lines, line{start: start, end: end, text: text[start:end]})
		start = end
	}
	return lines
}

var (
	fencePattern         = regexp.MustCompile(`^[\s]{0,3}(` + "`{3,}" + `|~{3,})`)
	indentedCodePattern  = regexp.MustCompile(`^( {4}|\t)[^\s]`)
	tableRowPattern      = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	stackTracePattern    = regexp.MustCompile(`^\s*(at\s|File\s"|Caused by:|\.{3}\s\d+\smore|Traceback \(most recent call last\))`)
	blankLinePattern     = regexp.MustCompile(`^\s*$`)
	trailingSpacePattern = regexp.MustCompile(`\s$`)
)

// lineScan carries the state of the line pass. A blank line inside an indented
// run keeps the run alive — a code block may contain empty lines — but a blank
// line is only protected once indented code resumes after it, so it is buffered
// rather than emitted eagerly.
type lineScan struct {
	spans []span
	// pending holds indented-code lines held back until a further indented line
	// confirms them.
	pending []span
	inFence bool
}

func pushSpan(spans []span, s span) []span {
	if s.end > s.start {
		return append(spans, s)
	}
	return spans
}

func (scan *lineScan) commitPending() {
	for _, s := range scan.pending {
		scan.spans = pushSpan(scan.spans, s)
	}
	scan.pending = nil
}

func (scan *lineScan) scanLine(ln line) {
	current := span{start: ln.start, end: ln.end}
	switch {
	case fencePattern.MatchString(ln.text):
		scan.commitPending()
		scan.spans = pushSpan(scan.spans, current)
		scan.inFence = !scan.inFence
	case scan.inFence:
		scan.spans = pushSpan(scan.spans, current)
	case indentedCodePattern.MatchString(ln.text):
		scan.commitPending()
		scan.spans = pushSpan(scan.spans, current)
	case blankLinePattern.MatchString(ln.text):
		scan.pending = append(scan.pending, current)
	case tableRowPattern.MatchString(ln.text) || stackTracePattern.MatchString(ln.text):
		scan.pending = nil
		scan.spans = pushSpan(scan.spans, current)
	default:
		scan.pending = nil
	}
}

// scanLines is line-oriented protection. Fences own every line between them
// including the closers, so a URL or table row inside a fenced block never
// fragments it. An unterminated fence protects everything after it; its lines
// are already in.
func scanLines(text string) []span {
	scan := &lineScan{}
	for _, ln := range splitLines(text) {
		scan.scanLine(ln)
	}
	return scan.spans
}

// inlinePatterns are inline constructs, each anchored tightly enough that
// ordinary prose does not match. Order is irrelevant because every match is
// merged by offset, not by precedence — longest coverage wins through the union,
// not through the list.
//
// Two of the TS patterns used lookaround, which RE2 has no equivalent for; both
// are handled by scanVersions and scanFilenames instead.
var inlinePatterns = []*regexp.Regexp{
	// Inline code, backtick-delimited, non-greedy so adjacent spans stay separate.
	regexp.MustCompile("`[^`\n]+`"),
	// URLs, including the query string and fragment.
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>()\[\]{}"']+`),
	// Bare hosts that read as URLs without a scheme.
	regexp.MustCompile(`(?i)\bwww\.[^\s<>()\[\]{}"']+`),
	// Windows paths.
	regexp.MustCompile(`\b[A-Za-z]:\\[^\s"'<>|]+`),
	// POSIX-ish paths: a segment followed by at least one slash-joined segment,
	// anchored on a leading `/`, `./`, `../`, or a dotted filename.
	regexp.MustCompile(`(\.{1,2}/|/)[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*/?`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)+\.[A-Za-z0-9]+\b`),
	// JSON or JS object/array literals, one nesting level of braces.
	regexp.MustCompile(`\{[^{}\n]*[:,][^{}\n]*\}`),
	regexp.MustCompile(`\[[^\[\]\n]*[:,"][^\[\]\n]*\]`),
	// One line of a pretty-printed object, whose braces sit on other lines.
	regexp.MustCompile(`(?m)^[ \t]*"([^"\\\n]|\\.)*"[ \t]*:.*$`),
	// A double-quoted string anywhere else; its contents are read back verbatim.
	regexp.MustCompile(`"([^"\\\n]|\\.)*"`),
	// XML and JSX elements.
	regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9.:-]*(\s[^<>]*)?/?>`),
	// Stack frame fragments appearing mid-line.
	regexp.MustCompile(`\([^\s()]+:\d+(:\d+)?\)`),
	regexp.MustCompile(`\b[A-Za-z0-9_.-]+:\d+(:\d+)?\b`),
	// Hex literals, UUIDs, long digit and hash runs.
	regexp.MustCompile(`\b0[xX][0-9a-fA-F]+\b`),
	regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`),
	regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`),
	regexp.MustCompile(`\b\d{7,}\b`),
	// Markdown structural markers. The marker only — the sentence after a bullet
	// or a header is prose and stays compressible.
	regexp.MustCompile(`(?m)^[ \t]*([-*+]|\d+[.)])[ \t]+`),
	regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+`),
	regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`),
	// Identifiers that are code by shape rather than by delimiter.
	regexp.MustCompile(`\b[A-Za-z_$][A-Za-z0-9_$]*\([^()\n]*\)`),
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9]*(_[a-z0-9]+)+\b`),
	regexp.MustCompile(`\$[A-Za-z_{][A-Za-z0-9_}]*`),
	regexp.MustCompile(`--?[A-Za-z][A-Za-z0-9-]*=[^\s]+`),
}

// versionPattern stands in for a lookbehind: the separator is captured so the
// reported span can start at the submatch instead of at the separator.
var versionPattern = regexp.MustCompile(`(^|[\s(=@])([v^~><=]{0,2}\d+\.\d+(\.\d+)?([-+][A-Za-z0-9.]+)?)`)

// scanVersions reports the version submatch and discards the separator that
// opened it. Consecutive versions still both match, because a version never
// consumes the character after it, so the separator for the next one is left
// unread — `v1.2.3 v4.5.6` yields two spans without any rescanning.
//
// Advancing from the end of the version instead would re-read the separator that
// terminated it. In `x=1.0=2.0` the shared `=` would then open the second match
// from one byte early, protecting `=2.0` where the TS protects `2.0`.
func scanVersions(text string) []span {
	spans := []span{}
	for _, match := range versionPattern.FindAllStringSubmatchIndex(text, -1) {
		spans = append(spans, span{start: match[4], end: match[5]})
	}
	return spans
}

// filenamePattern stands in for a lookahead: the terminator is captured so it
// can be excluded from the reported span.
var filenamePattern = regexp.MustCompile(`\b([A-Za-z0-9_-]+\.([A-Za-z]{1,5}[A-Za-z0-9]{0,3}))\b([\s:,)\]}]|$)`)

func scanFilenames(text string) []span {
	spans := []span{}
	for _, match := range filenamePattern.FindAllStringSubmatchIndex(text, -1) {
		spans = append(spans, span{start: match[2], end: match[3]})
	}
	return spans
}

func scanInline(text string) []span {
	spans := []span{}
	for _, pattern := range inlinePatterns {
		for _, match := range pattern.FindAllStringIndex(text, -1) {
			if match[1] == match[0] {
				continue
			}
			spans = append(spans, span{start: match[0], end: match[1]})
		}
	}
	spans = append(spans, scanFilenames(text)...)
	spans = append(spans, scanVersions(text)...)
	return spans
}

// mergeSpans collapses overlapping and touching spans into one, so protection
// never fragments. Sorted by start, then by end descending, so a longer span at
// the same offset is merged first.
func mergeSpans(spans []span) []span {
	ordered := make([]span, len(spans))
	copy(ordered, spans)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].start != ordered[j].start {
			return ordered[i].start < ordered[j].start
		}
		return ordered[i].end > ordered[j].end
	})
	merged := []span{}
	for _, s := range ordered {
		if len(merged) > 0 && s.start <= merged[len(merged)-1].end {
			last := &merged[len(merged)-1]
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

func clampSpans(spans []span, length int) []span {
	clamped := []span{}
	for _, s := range spans {
		start := min(max(0, s.start), length)
		end := max(start, min(s.end, length))
		if end > start {
			clamped = append(clamped, span{start: start, end: end})
		}
	}
	return clamped
}

func fillGaps(protectedSpans []span, length int) []Region {
	regions := []Region{}
	cursor := 0
	for _, s := range protectedSpans {
		if s.start > cursor {
			regions = append(regions, Region{Kind: RegionProse, Start: cursor, End: s.start})
		}
		regions = append(regions, Region{Kind: RegionProtected, Start: s.start, End: s.end})
		cursor = s.end
	}
	if cursor < length {
		regions = append(regions, Region{Kind: RegionProse, Start: cursor, End: length})
	}
	return regions
}

// ClassifyRegions splits text into regions that tile it exactly: the first
// starts at 0, each region's end is the next one's start, and the last ends at
// len(text). Reconstructing the slices in order yields the input back.
//
// Anything matching a code-shaped pattern resolves to protected, because a false
// positive costs only savings while a false negative corrupts a code block, a
// path or a stack trace that the model needs verbatim.
func ClassifyRegions(text string) []Region {
	if text == "" {
		return []Region{}
	}
	found := append(scanLines(text), scanInline(text)...)
	return fillGaps(mergeSpans(clampSpans(found, len(text))), len(text))
}
