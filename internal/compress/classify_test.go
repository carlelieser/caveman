package compress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// The golden files record offsets in UTF-16 code units, as the TS oracle produced
// them. Go works in bytes, so the comparison converts rather than assuming the two
// coincide — most fixture text is ASCII, where they do, but the unicode fixtures
// exist precisely because some of it is not.

type goldenWord struct {
	Text      string `json:"text"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	WordClass string `json:"wordClass"`
}

type goldenRegion struct {
	Kind  string `json:"kind"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type taggerCase struct {
	ID    string       `json:"id"`
	Text  string       `json:"text"`
	Words []goldenWord `json:"words"`
}

type regionCase struct {
	ID      string         `json:"id"`
	Text    string         `json:"text"`
	Regions []goldenRegion `json:"regions"`
}

type unicodeCase struct {
	ID      string         `json:"id"`
	Text    string         `json:"text"`
	Regions []goldenRegion `json:"regions"`
	Words   []goldenWord   `json:"words"`
}

func goldenPath(name string) string {
	return filepath.Join("..", "..", "testdata", "golden", name)
}

func readGolden(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

// utf16ToByte maps a UTF-16 code-unit offset in text to a byte offset.
func utf16ToByte(text string, u16 int) int {
	if u16 <= 0 {
		return 0
	}
	units, bytes := 0, 0
	for _, r := range text {
		if units >= u16 {
			return bytes
		}
		units += utf16.RuneLen(r)
		bytes += utf8.RuneLen(r)
	}
	if units >= u16 {
		return bytes
	}
	return len(text)
}

// byteToUTF16 maps a byte offset in text to a UTF-16 code-unit offset.
func byteToUTF16(text string, b int) int {
	units := 0
	for i, r := range text {
		if i >= b {
			return units
		}
		units += utf16.RuneLen(r)
	}
	return units
}

func toRegions(text string, in []goldenRegion) []Region {
	out := make([]Region, 0, len(in))
	for _, r := range in {
		out = append(out, Region{
			Kind:  r.Kind,
			Start: utf16ToByte(text, r.Start),
			End:   utf16ToByte(text, r.End),
		})
	}
	return out
}

// compare reports the first differences between produced and expected words,
// with the produced offsets converted back to UTF-16 for a like-for-like diff.
func compare(t *testing.T, id, text string, got []ClassifiedWord, want []goldenWord) []string {
	t.Helper()
	diffs := []string{}
	for i := 0; i < len(got) || i < len(want); i++ {
		switch {
		case i >= len(got):
			w := want[i]
			diffs = append(diffs, fmt.Sprintf(
				"  [%d] missing: want %q [%d,%d) %s", i, w.Text, w.Start, w.End, w.WordClass))
		case i >= len(want):
			g := got[i]
			diffs = append(diffs, fmt.Sprintf(
				"  [%d] extra:   got  %q [%d,%d) %s", i,
				g.Text, byteToUTF16(text, g.Start), byteToUTF16(text, g.End), g.WordClass))
		default:
			g, w := got[i], want[i]
			gs, ge := byteToUTF16(text, g.Start), byteToUTF16(text, g.End)
			if g.Text != w.Text || gs != w.Start || ge != w.End || string(g.WordClass) != w.WordClass {
				diffs = append(diffs, fmt.Sprintf(
					"  [%d] got  %q [%d,%d) %s\n       want %q [%d,%d) %s",
					i, g.Text, gs, ge, g.WordClass, w.Text, w.Start, w.End, w.WordClass))
			}
		}
	}
	return diffs
}

// TestClassifyWordsMatchesGolden is the primary gate: the words array must match
// the TS oracle exactly for every node, since it is what decides compression.
func TestClassifyWordsMatchesGolden(t *testing.T) {
	var cases []taggerCase
	readGolden(t, "tagger.json", &cases)
	var regions []regionCase
	readGolden(t, "regions.json", &regions)

	byID := map[string][]goldenRegion{}
	for _, r := range regions {
		byID[r.ID] = r.Regions
	}

	matched := 0
	for _, c := range cases {
		regs, ok := byID[c.ID]
		if !ok {
			t.Fatalf("no regions recorded for %q", c.ID)
		}
		got := ClassifyWords(c.Text, toRegions(c.Text, regs))
		diffs := compare(t, c.ID, c.Text, got, c.Words)
		if len(diffs) == 0 {
			matched++
			continue
		}
		t.Errorf("%s: %d word difference(s)\n%s", c.ID, len(diffs), joinLimit(diffs, 12))
	}
	t.Logf("tagger.json: %d/%d nodes match exactly", matched, len(cases))
}

// TestClassifyWordsUnicode pins the two substitutions the Go port makes for
// platform APIs: uniseg for Intl.Segmenter, and a generated range table for
// \p{Extended_Pictographic}. The ASCII corpus cannot exercise either.
func TestClassifyWordsUnicode(t *testing.T) {
	var cases []unicodeCase
	readGolden(t, "unicode.json", &cases)

	matched := 0
	for _, c := range cases {
		got := ClassifyWords(c.Text, toRegions(c.Text, c.Regions))
		diffs := compare(t, c.ID, c.Text, got, c.Words)
		if len(diffs) == 0 {
			matched++
			continue
		}
		t.Errorf("%s: %q\n%d word difference(s)\n%s",
			c.ID, c.Text, len(diffs), joinLimit(diffs, 12))
	}
	t.Logf("unicode.json: %d/%d cases match exactly", matched, len(cases))
}

// TestSliceInvariant is the guarantee the rest of the pipeline relies on:
// every returned word can be cut back out of the source at its own offsets.
func TestSliceInvariant(t *testing.T) {
	var cases []taggerCase
	readGolden(t, "tagger.json", &cases)
	var regions []regionCase
	readGolden(t, "regions.json", &regions)
	byID := map[string][]goldenRegion{}
	for _, r := range regions {
		byID[r.ID] = r.Regions
	}
	for _, c := range cases {
		for _, w := range ClassifyWords(c.Text, toRegions(c.Text, byID[c.ID])) {
			if c.Text[w.Start:w.End] != w.Text {
				t.Errorf("%s: text[%d:%d] = %q, want %q",
					c.ID, w.Start, w.End, c.Text[w.Start:w.End], w.Text)
			}
		}
	}
}

func joinLimit(lines []string, limit int) string {
	out := ""
	for i, l := range lines {
		if i == limit {
			out += fmt.Sprintf("  ... and %d more", len(lines)-limit)
			break
		}
		out += l + "\n"
	}
	return out
}
