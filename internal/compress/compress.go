package compress

import (
	"sort"
	"strings"
)

// CompressRole is where the text came from, so removal can be scoped by origin.
type CompressRole string

const (
	RoleUser      CompressRole = "user"
	RoleAssistant CompressRole = "assistant"
	RoleSystem    CompressRole = "system"
)

// CompressKind is which kind of IR node the text was taken from.
type CompressKind string

const (
	KindText       CompressKind = "text"
	KindToolResult CompressKind = "tool_result"
)

type CompressContext struct {
	Role CompressRole
	Kind CompressKind
}

type CompressionStats struct {
	WordsIn  int
	WordsOut int
	CharsIn  int
	CharsOut int
	// CharsProse is the characters in prose regions, the only ones a level can
	// remove. This is the ceiling on what compressing this text could save.
	CharsProse int
	// IsUncompressed is true when an invariant forced the original text back.
	IsUncompressed bool
}

type CompressionResult struct {
	Text  string
	Stats CompressionStats
}

type CompressRequest struct {
	Text    string
	Level   Level
	Context CompressContext
}

func identityResult(text string, wordsIn, charsProse int) CompressionResult {
	return CompressionResult{
		Text: text,
		Stats: CompressionStats{
			WordsIn:        wordsIn,
			WordsOut:       wordsIn,
			CharsIn:        len(text),
			CharsOut:       len(text),
			CharsProse:     charsProse,
			IsUncompressed: true,
		},
	}
}

// countProse totals the prose regions, which is what classification left open.
func countProse(regions []Region) int {
	total := 0
	for _, region := range regions {
		if region.Kind == RegionProse {
			total += region.End - region.Start
		}
	}
	return total
}

// ProseLength is the prose share of a block that is not being compressed.
func ProseLength(text string) int {
	return countProse(ClassifyRegions(text))
}

type assembly struct {
	parts               []string
	pendingGap          string
	hasEmitted          bool
	hasDroppedSinceEmit bool
}

func (a *assembly) absorbGap(gap string) {
	a.pendingGap += gap
}

func (a *assembly) emitText(text string) {
	a.parts = append(a.parts, a.separatorFor(), text)
	a.pendingGap = ""
	a.hasEmitted = true
	a.hasDroppedSinceEmit = false
}

// separatorFor suppresses a joining space after a region that already ends in
// whitespace. A protected region carries its own trailing whitespace — a list
// marker is "- ", a table block ends in its newline — so the separator a drop
// would otherwise contribute would double it. Emitting nothing keeps the
// region's bytes exactly as they were written.
func (a *assembly) separatorFor() string {
	separator := a.gapBefore()
	if separator != " " || len(a.parts) == 0 {
		return separator
	}
	previous := a.parts[len(a.parts)-1]
	if trailingSpacePattern.MatchString(previous) {
		return ""
	}
	return separator
}

// gapBefore is the separator to put in front of the next emission. Nothing was
// dropped, so the gap stands as written; a drop turns it into a joining separator
// instead. Before the first emission there is nothing to join to, so a gap left
// by a dropped opening word collapses away rather than indenting the block.
func (a *assembly) gapBefore() string {
	if !a.hasDroppedSinceEmit {
		return a.pendingGap
	}
	if !a.hasEmitted {
		return leadingGap(a.pendingGap)
	}
	return joinGap(a.pendingGap)
}

// leadingGap keeps a block that lost its first word at the indentation it had,
// but not the space the word used to occupy, so the text still starts at the left
// margin it started at before compression.
func leadingGap(gap string) string {
	collapsed := collapseWhitespace(stripLeadingPunctuation(gap))
	if collapsed == " " {
		return ""
	}
	return collapsed
}

// unit is one emittable piece of the source in offset order: either a protected
// region that must survive byte-identically, or a classified word that may be
// dropped.
type unit struct {
	start       int
	end         int
	text        string
	isProtected bool
}

// assemble rebuilds the text from the units that survive. Leading and trailing
// gaps are preserved so surrounding layout survives.
//
// A protected region is emitted whole and marks the boundary as clean, so a word
// dropped just before it cannot pull punctuation out of the region that follows —
// the region's own bytes are already committed to the output.
func assemble(text string, units []unit, dropped map[int]struct{}) string {
	state := &assembly{}
	cursor := 0
	for index, u := range units {
		state.absorbGap(text[cursor:u.start])
		switch {
		case u.isProtected:
			if u.text != "" {
				state.emitText(u.text)
			}
		default:
			if _, isDropped := dropped[index]; isDropped {
				state.hasDroppedSinceEmit = true
			} else {
				state.emitText(u.text)
			}
		}
		cursor = u.end
	}
	state.parts = append(state.parts, trailingGap(state, text[cursor:]))
	return strings.Join(state.parts, "")
}

// trailingGap is the gap after the last unit. A drop in it still strands
// punctuation, so it gets the same treatment as any other gap, but it is never
// used to join two words and so keeps whatever trailing layout the block had.
func trailingGap(state *assembly, tail string) string {
	gap := state.pendingGap + tail
	if !state.hasDroppedSinceEmit {
		return gap
	}
	return terminalMarkOf(gap) + collapseWhitespace(stripLeadingPunctuation(gap))
}

func buildUnits(text string, regions []Region, words []ClassifiedWord) []unit {
	units := []unit{}
	for _, region := range regions {
		if region.Kind != RegionProtected {
			continue
		}
		units = append(units, unit{
			start:       region.Start,
			end:         region.End,
			text:        text[region.Start:region.End],
			isProtected: true,
		})
	}
	for _, word := range words {
		units = append(units, unit{start: word.Start, end: word.End, text: word.Text})
	}
	// Units never overlap — words come only from prose regions — so ordering by
	// start alone is total, with end as a tiebreak for the empty-span case.
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].start != units[j].start {
			return units[i].start < units[j].start
		}
		return units[i].end < units[j].end
	})
	return units
}

func selectDropped(units []unit, words []ClassifiedWord, level Level) map[int]struct{} {
	classByOffset := make(map[int]WordClass, len(words))
	for _, word := range words {
		classByOffset[word.Start] = word.WordClass
	}
	dropped := map[int]struct{}{}
	for index, u := range units {
		if u.isProtected {
			continue
		}
		wordClass, ok := classByOffset[u.start]
		if ok && IsRemovable(level, wordClass) {
			dropped[index] = struct{}{}
		}
	}
	return dropped
}

func countWords(units []unit) int {
	total := 0
	for _, u := range units {
		if !u.isProtected {
			total++
		}
	}
	return total
}

func buildResult(candidate string, request CompressRequest, wordsIn, droppedCount, charsProse int) CompressionResult {
	// Removal can only shorten text, so a longer candidate is an assembly fault.
	if len(candidate) > len(request.Text) {
		return identityResult(request.Text, wordsIn, charsProse)
	}
	return CompressionResult{
		Text: candidate,
		Stats: CompressionStats{
			WordsIn:    wordsIn,
			WordsOut:   wordsIn - droppedCount,
			CharsIn:    len(request.Text),
			CharsOut:   len(candidate),
			CharsProse: charsProse,
		},
	}
}

// CompressText rewrites a block by removing whole grammatical classes. Text is
// split into protected and prose regions first, so code, paths, JSON and stack
// traces are copied through byte-identically; only words inside prose are
// classified, and only the classes the level names are removed.
//
// A block of nothing but removable words compresses to nothing. Whether an empty
// block can go on the wire is RunPipeline's question, not this one's.
func CompressText(request CompressRequest) CompressionResult {
	regions := ClassifyRegions(request.Text)
	charsProse := countProse(regions)
	words := ClassifyWords(request.Text, regions)
	units := buildUnits(request.Text, regions, words)
	wordsIn := countWords(units)
	if wordsIn == 0 {
		return identityResult(request.Text, wordsIn, charsProse)
	}

	dropped := selectDropped(units, words, request.Level)
	if len(dropped) == 0 {
		return identityResult(request.Text, wordsIn, charsProse)
	}

	candidate := assemble(request.Text, units, dropped)
	return buildResult(candidate, request, wordsIn, len(dropped), charsProse)
}
