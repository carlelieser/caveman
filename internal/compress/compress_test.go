package compress

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

type goldenStats struct {
	WordsIn        int  `json:"wordsIn"`
	WordsOut       int  `json:"wordsOut"`
	CharsIn        int  `json:"charsIn"`
	CharsOut       int  `json:"charsOut"`
	CharsProse     int  `json:"charsProse"`
	IsUncompressed bool `json:"isUncompressed"`
}

type compressionCase struct {
	ID    string      `json:"id"`
	Level string      `json:"level"`
	Role  string      `json:"role"`
	Kind  string      `json:"kind"`
	In    string      `json:"in"`
	Out   string      `json:"out"`
	Stats goldenStats `json:"stats"`
}

// utf16Len is how the TS counted a string, so the recorded char totals compare
// like for like against Go's byte counts.
func utf16Len(text string) int {
	return len(utf16.Encode([]rune(text)))
}

// TestCompressTextMatchesGolden is the primary gate: every node at every level
// must come out byte-identical to what the TS oracle produced.
func TestCompressTextMatchesGolden(t *testing.T) {
	var cases []compressionCase
	readGolden(t, "compression.json", &cases)

	matched := 0
	for i, c := range cases {
		got := CompressText(CompressRequest{
			Text:    c.In,
			Level:   Level(c.Level),
			Context: CompressContext{Role: CompressRole(c.Role), Kind: CompressKind(c.Kind)},
		})
		if *update {
			cases[i].Out = got.Text
			cases[i].Stats = statsOf(c.In, got)
			continue
		}
		diffs := diffCase(c, got)
		if len(diffs) == 0 {
			matched++
			continue
		}
		t.Errorf("%s [%s]: %d difference(s)\n  in:   %q\n%s",
			c.ID, c.Level, len(diffs), c.In, joinLimit(diffs, 8))
	}
	if *update {
		writeGolden(t, "compression.json", cases)
		return
	}
	reportMismatch(t, "compression.json", matched, len(cases))
	t.Logf("compression.json: %d/%d cases match exactly", matched, len(cases))
}

// statsOf mirrors the shape the oracle recorded, so a regenerated file differs
// only where compression did.
func statsOf(in string, got CompressionResult) goldenStats {
	return goldenStats{
		WordsIn:        got.Stats.WordsIn,
		WordsOut:       got.Stats.WordsOut,
		CharsIn:        utf16Len(in),
		CharsOut:       utf16Len(got.Text),
		CharsProse:     proseUTF16(in),
		IsUncompressed: got.Stats.IsUncompressed,
	}
}

func diffCase(c compressionCase, got CompressionResult) []string {
	diffs := []string{}
	if got.Text != c.Out {
		diffs = append(diffs, fmt.Sprintf("  out:  got  %q\n        want %q", got.Text, c.Out))
	}
	stats := []struct {
		name string
		got  int
		want int
	}{
		{"wordsIn", got.Stats.WordsIn, c.Stats.WordsIn},
		{"wordsOut", got.Stats.WordsOut, c.Stats.WordsOut},
		{"charsIn", utf16Len(c.In), c.Stats.CharsIn},
		{"charsOut", utf16Len(got.Text), c.Stats.CharsOut},
		{"charsProse", proseUTF16(c.In), c.Stats.CharsProse},
	}
	for _, s := range stats {
		if s.got != s.want {
			diffs = append(diffs, fmt.Sprintf("  %s: got %d, want %d", s.name, s.got, s.want))
		}
	}
	if got.Stats.IsUncompressed != c.Stats.IsUncompressed {
		diffs = append(diffs, fmt.Sprintf("  isUncompressed: got %v, want %v",
			got.Stats.IsUncompressed, c.Stats.IsUncompressed))
	}
	return diffs
}

// proseUTF16 re-measures the prose share in the oracle's units, since the stat is
// a total rather than an offset and so cannot be converted after the fact.
func proseUTF16(text string) int {
	units := 0
	for _, r := range ClassifyRegions(text) {
		if r.Kind == RegionProse {
			units += byteToUTF16(text, r.End) - byteToUTF16(text, r.Start)
		}
	}
	return units
}

// TestSpotChecks pins the sentences the level design is stated in terms of, so a
// regression shows up as a readable failure rather than a count.
func TestSpotChecks(t *testing.T) {
	checks := []struct {
		text  string
		level Level
		want  string
	}{
		{"The man went to the store.", LevelLight, "man went to store."},
		{"an abandoned building was sold", LevelCaveman, "building sold"},
		{"The very large dog quickly ate the food.", LevelCaveman, "dog ate food."},
	}
	for _, check := range checks {
		got := CompressText(CompressRequest{Text: check.text, Level: check.level}).Text
		if got != check.want {
			t.Errorf("%q [%s]: got %q, want %q", check.text, check.level, got, check.want)
		}
	}
}

// TestSubordinatorsSurvive covers the words whose removal would invert a clause's
// meaning: no level may drop them, and `not` is in the same position.
func TestSubordinatorsSurvive(t *testing.T) {
	sentences := []string{
		"Do not proceed if the tests fail.",
		"Stop unless the build is green.",
		"It failed because the socket was closed.",
	}
	words := []string{"not", "if", "unless", "because"}
	for _, sentence := range sentences {
		for _, level := range LevelNames {
			got := CompressText(CompressRequest{Text: sentence, Level: level}).Text
			for _, word := range words {
				if !strings.Contains(sentence, " "+word+" ") {
					continue
				}
				if !strings.Contains(got, word) {
					t.Errorf("%q [%s]: %q was dropped, got %q", sentence, level, word, got)
				}
			}
		}
	}
}

// TestLevelsAreNested pins light ⊂ moderate ⊂ caveman, which is what makes output
// length non-increasing as the level rises.
func TestLevelsAreNested(t *testing.T) {
	var cases []proseCase
	readGolden(t, "regions.json", &cases)
	for _, c := range cases {
		lengths := map[Level]int{}
		for _, level := range LevelNames {
			lengths[level] = len(CompressText(CompressRequest{Text: c.Text, Level: level}).Text)
		}
		if lengths[LevelModerate] > lengths[LevelLight] || lengths[LevelCaveman] > lengths[LevelModerate] {
			t.Errorf("%s: lengths not non-increasing: light=%d moderate=%d caveman=%d",
				c.ID, lengths[LevelLight], lengths[LevelModerate], lengths[LevelCaveman])
		}
	}
	for class := range removable[LevelLight] {
		if !IsRemovable(LevelModerate, class) || !IsRemovable(LevelCaveman, class) {
			t.Errorf("%s is removable at light but not above it", class)
		}
	}
	for class := range removable[LevelModerate] {
		if !IsRemovable(LevelCaveman, class) {
			t.Errorf("%s is removable at moderate but not at caveman", class)
		}
	}
}

// TestProtectedRegionsSurviveVerbatim is the guarantee the whole design rests on:
// whatever a region protected is present in the output exactly as it was written.
func TestProtectedRegionsSurviveVerbatim(t *testing.T) {
	var cases []proseCase
	readGolden(t, "regions.json", &cases)
	for _, c := range cases {
		for _, level := range LevelNames {
			out := CompressText(CompressRequest{Text: c.Text, Level: level}).Text
			for _, r := range ClassifyRegions(c.Text) {
				if r.Kind != RegionProtected {
					continue
				}
				if !strings.Contains(out, c.Text[r.Start:r.End]) {
					t.Errorf("%s [%s]: protected %q missing from output",
						c.ID, level, c.Text[r.Start:r.End])
				}
			}
		}
	}
}

// TestUnicodeQuirks pins behaviour the ASCII corpus cannot reach, verified case
// by case against the TS. Three of these are oracle quirks rather than intended
// results — an emoji beside a dropped word survives, a dropped word before one
// can lose its separator, and precomposed and decomposed `café` compress
// differently — and byte-fidelity to the oracle comes before fixing them.
//
// The flag and skin-tone cases are what separates Extended_Pictographic from the
// wider table the tagger carries: both hold codepoints that are `\p{S}` without
// being pictographic, so the union would strip a leading flag the TS keeps.
func TestUnicodeQuirks(t *testing.T) {
	checks := []struct {
		text string
		want string
	}{
		{"it is a \U0001F389 party", "\U0001F389 party"},
		{"the café was quite busy", "café"},
		{"the café was quite busy", "café "},
		{"The family \U0001F468‍\U0001F469‍\U0001F467‍\U0001F466 was very happy about the result.",
			"family\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466 result."},
		{"the flag \U0001F1EF\U0001F1F5 is red and white", "flag \U0001F1EF\U0001F1F5"},
		{"the developer \U0001F469\U0001F3FD‍\U0001F4BB quickly fixed the bug",
			"developer\U0001F469\U0001F3FD‍\U0001F4BB fixed bug"},
	}
	for _, check := range checks {
		got := CompressText(CompressRequest{Text: check.text, Level: LevelCaveman}).Text
		if got != check.want {
			t.Errorf("%q: got %q, want %q", check.text, got, check.want)
		}
	}
}
