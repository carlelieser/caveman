package compress

import (
	"fmt"
	"testing"
)

// proseCase adds the recorded prose total to the region case shape, which the
// tagger gate does not need and so does not read.
type proseCase struct {
	ID          string         `json:"id"`
	Text        string         `json:"text"`
	Regions     []goldenRegion `json:"regions"`
	ProseLength int            `json:"proseLength"`
}

// TestClassifyRegionsMatchesGolden is the regions gate: the region tiling must
// match the TS oracle span for span, since it decides what compression may touch.
func TestClassifyRegionsMatchesGolden(t *testing.T) {
	var cases []proseCase
	readGolden(t, "regions.json", &cases)

	matched, prose, protectedCount := 0, 0, 0
	for _, c := range cases {
		got := ClassifyRegions(c.Text)
		diffs := compareRegions(c.Text, got, c.Regions)
		for _, r := range c.Regions {
			if r.Kind == RegionProse {
				prose++
			} else {
				protectedCount++
			}
		}
		if len(diffs) == 0 {
			matched++
			continue
		}
		t.Errorf("%s: %d region difference(s)\n%s", c.ID, len(diffs), joinLimit(diffs, 12))
	}
	t.Logf("regions.json: %d/%d nodes match exactly (%d prose, %d protected spans)",
		matched, len(cases), prose, protectedCount)
}

// TestRegionsTile pins the invariant the assembler relies on: regions cover the
// source with no gap and no overlap, so reassembling them yields the input back.
func TestRegionsTile(t *testing.T) {
	var cases []proseCase
	readGolden(t, "regions.json", &cases)
	for _, c := range cases {
		cursor := 0
		rebuilt := ""
		for _, r := range ClassifyRegions(c.Text) {
			if r.Start != cursor {
				t.Fatalf("%s: region starts at %d, want %d", c.ID, r.Start, cursor)
			}
			rebuilt += c.Text[r.Start:r.End]
			cursor = r.End
		}
		if cursor != len(c.Text) || rebuilt != c.Text {
			t.Errorf("%s: tiling does not reconstruct the source", c.ID)
		}
	}
}

func TestProseLengthMatchesGolden(t *testing.T) {
	var cases []proseCase
	readGolden(t, "regions.json", &cases)
	// ProseLength is a total, not an offset, so the two conventions are compared
	// by re-measuring each prose region in UTF-16 rather than by converting it.
	for _, c := range cases {
		units := 0
		for _, r := range ClassifyRegions(c.Text) {
			if r.Kind == RegionProse {
				units += byteToUTF16(c.Text, r.End) - byteToUTF16(c.Text, r.Start)
			}
		}
		if units != c.ProseLength {
			t.Errorf("%s: proseLength = %d, want %d", c.ID, units, c.ProseLength)
		}
		if got, want := ProseLength(c.Text), proseBytes(c.Text); got != want {
			t.Errorf("%s: ProseLength = %d bytes, want %d", c.ID, got, want)
		}
	}
}

func compareRegions(text string, got []Region, want []goldenRegion) []string {
	diffs := []string{}
	for i := 0; i < len(got) || i < len(want); i++ {
		switch {
		case i >= len(got):
			w := want[i]
			diffs = append(diffs, fmt.Sprintf("  [%d] missing: want %s [%d,%d)", i, w.Kind, w.Start, w.End))
		case i >= len(want):
			g := got[i]
			diffs = append(diffs, fmt.Sprintf("  [%d] extra:   got  %s [%d,%d) %q", i,
				g.Kind, byteToUTF16(text, g.Start), byteToUTF16(text, g.End), text[g.Start:g.End]))
		default:
			g, w := got[i], want[i]
			gs, ge := byteToUTF16(text, g.Start), byteToUTF16(text, g.End)
			if g.Kind != w.Kind || gs != w.Start || ge != w.End {
				diffs = append(diffs, fmt.Sprintf("  [%d] got  %s [%d,%d) %q\n       want %s [%d,%d) %q",
					i, g.Kind, gs, ge, text[g.Start:g.End],
					w.Kind, w.Start, w.End, sliceUTF16(text, w.Start, w.End)))
			}
		}
	}
	return diffs
}

func proseBytes(text string) int {
	total := 0
	for _, r := range ClassifyRegions(text) {
		if r.Kind == RegionProse {
			total += r.End - r.Start
		}
	}
	return total
}

func sliceUTF16(text string, start, end int) string {
	return text[utf16ToByte(text, start):utf16ToByte(text, end)]
}

// TestLookaroundWorkarounds pins the two patterns RE2 cannot express directly.
// The golden corpus reaches neither edge — it holds no two versions sharing a
// separator, and no dotted word whose extension is rejected by its terminator —
// so without these the substitutions could regress to a naive FindAll and the
// primary gate would stay green. Every expectation here was read off the TS.
func TestLookaroundWorkarounds(t *testing.T) {
	checks := []struct {
		scan  func(string) []span
		text  string
		spans []span
	}{
		// Consecutive versions both match: a version never consumes the character
		// after it, so the separator that opens the next one is left unread.
		{scanVersions, "v1.2.3 v4.5.6", []span{{0, 6}, {7, 13}}},
		// One `=` terminates the first version and opens the second. Rescanning
		// from the end of a version would protect `=2.0` here instead of `2.0`.
		{scanVersions, "x=1.0=2.0", []span{{2, 5}, {6, 9}}},
		{scanVersions, "a@2.3@1.2.3-beta.1", []span{{2, 5}, {6, 18}}},
		{scanVersions, "use 1.2 and ^2.0.0", []span{{4, 7}, {12, 18}}},
		// The separator is required, so a version glued to a word is not one.
		{scanVersions, "x1.2.3", nil},
		{scanVersions, "1.2.3-beta.1 ok", []span{{0, 12}}},
		// The terminator is matched but not reported, so the next filename on the
		// line still starts inside what the terminator consumed.
		{scanFilenames, "see foo.ts, bar.js) end", []span{{4, 10}, {12, 18}}},
		{scanFilenames, "file.tsx:12", []span{{0, 8}}},
		// `a.b` fails the terminator, and the scan resumes inside it rather than
		// past it, which is what leaves `c.d`.
		{scanFilenames, "a.b.c.d ", []span{{4, 7}}},
		{scanFilenames, "no.match!", nil},
	}
	for _, check := range checks {
		got := check.scan(check.text)
		if len(got) != len(check.spans) {
			t.Errorf("%q: got %v, want %v", check.text, got, check.spans)
			continue
		}
		for i, want := range check.spans {
			if got[i] != want {
				t.Errorf("%q: span %d = %v, want %v", check.text, i, got[i], want)
			}
		}
	}
}
