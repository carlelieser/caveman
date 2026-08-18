package compress

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/carlelieser/caveman/internal/policy"
)

// The golden corpus is derived from recorded request fixtures, so it holds no
// empty string, no whitespace-only node, no 2,000-character run, and no mixed
// script line. These samples are the edge cases the oracle's own invariant
// suites ran against, kept here because the properties they pin — sliceable
// offsets, a gapless tiling, determinism — hold for every input rather than
// only for the corpus.
var samples = []string{
	"",
	"   ",
	"\n\n\t ",
	"hello world",
	"plain prose with no structure at all",
	"The man has quickly gone to the very large store.",
	"I need you to book a flight, and the book is on the table.",
	"She is running because they were tired and he could not sleep.",
	"John visited Paris in March 2024 with 3 friends.",
	"Don't stop believing! It's fine.",
	"emoji \U0001F468‍\U0001F469‍\U0001F467‍\U0001F466 survives compression \U0001F1EF\U0001F1F5 intact here",
	"日本語のテキストです これはテストです",
	"中文测试 mixed with English words",
	"café naïve résumé combining marks",
	"á combining mark before the noun",
	"multi\n\nparagraph\n\ntext with several lines",
	"a — b and x…y and “quoted” text",
	"The value is 42 and the id is abc-def.",
	"Here is code:\n```ts\nconst a = the(b);\n```\nand the rest of the prose.",
	"Check src/a/b.ts and https://x.example/a?b=1 for the details.",
	"trailing whitespace   ",
	"   leading whitespace",
	"the the the the the",
	"```ts\nconst value = compute(1, 2);\n```",
	"~~~\nraw block\n~~~",
	"text before\n```js\ncode()\n```\ntext after",
	"an unterminated ```fence\nand its body",
	"    indented code line\nand prose after",
	"\ttab indented code",
	"inline `value` code",
	"https://api.example.com/v1/x?y=1&z=2",
	"./a/b and ../c/d and /etc/hosts",
	"C:\\Users\\me\\file.txt on windows",
	`{"user_id": 4}`,
	`a json blob {"a": 1, "b": 2} inline`,
	`<Component prop="x" />`,
	`html <div class="a">text</div> here`,
	"at Foo.bar (app.js:10:5)",
	`File "x.py", line 3`,
	"0xDEADBEEF and 550e8400-e29b-41d4-a716-446655440000",
	"version v2.1.0-rc3 and ^14.16.0 and ~1.2.3",
	"| col | col |\n| --- | --- |\n| a | b |",
	"- bullet one\n- bullet two",
	"1. first\n2. second",
	"# Header\n## Subheader\n\nbody text",
	"> quoted line\n> more quote",
	"a\n\nb\n\nc",
	strings.Repeat("x", 3000),
	"mixed\n```\ncode\n```\n| t | t |\nat Foo (a.js:1:2)\nhttps://x.io/a?b=1\nprose the end.",
}

func classify(text string) []ClassifiedWord {
	return ClassifyWords(text, ClassifyRegions(text))
}

// Every word must cut back out of the source at its own offsets, be non-empty,
// and sit in ascending non-overlapping order inside the text.
func TestWordOffsetsAreSliceableAcrossSamples(t *testing.T) {
	for _, sample := range samples {
		words := classify(sample)
		for index, word := range words {
			if word.Start < 0 || word.End > len(sample) {
				t.Errorf("%q: word %d spans [%d,%d) outside the text",
					sample, index, word.Start, word.End)
				continue
			}
			if word.End <= word.Start || word.Text == "" {
				t.Errorf("%q: word %d is empty at [%d,%d)", sample, index, word.Start, word.End)
			}
			if got := sample[word.Start:word.End]; got != word.Text {
				t.Errorf("%q: text[%d:%d] = %q, want %q", sample, word.Start, word.End, got, word.Text)
			}
			if index > 0 && words[index-1].End > word.Start {
				t.Errorf("%q: word %d starts at %d, before word %d ends at %d",
					sample, index, word.Start, index-1, words[index-1].End)
			}
		}
	}
}

// A word boundary that fell inside a grapheme cluster would split an emoji
// sequence or strand a combining mark from its base.
func TestWordsNeverSplitAGraphemeCluster(t *testing.T) {
	const text = "keep \U0001F468‍\U0001F469‍\U0001F467‍\U0001F466 the family and \U0001F1EF\U0001F1F5 and á mark"
	boundaries := map[int]struct{}{0: {}, len(text): {}}
	offset := 0
	for graphemes := uniseg.NewGraphemes(text); graphemes.Next(); {
		offset += len(graphemes.Str())
		boundaries[offset] = struct{}{}
	}
	for _, word := range classify(text) {
		if _, ok := boundaries[word.Start]; !ok {
			t.Errorf("word %q starts at %d, inside a grapheme cluster", word.Text, word.Start)
		}
		if _, ok := boundaries[word.End]; !ok {
			t.Errorf("word %q ends at %d, inside a grapheme cluster", word.Text, word.End)
		}
	}
}

// Regions must tile the source with no gap and no overlap, so reassembling them
// yields the input back byte for byte.
func TestRegionsTileEverySample(t *testing.T) {
	for _, sample := range samples {
		regions := ClassifyRegions(sample)
		if sample == "" {
			if len(regions) != 0 {
				t.Errorf("empty input produced %d regions", len(regions))
			}
			continue
		}
		if regions[0].Start != 0 {
			t.Errorf("%q: first region starts at %d", sample, regions[0].Start)
		}
		if last := regions[len(regions)-1]; last.End != len(sample) {
			t.Errorf("%q: last region ends at %d, want %d", sample, last.End, len(sample))
		}
		rebuilt := strings.Builder{}
		for index, region := range regions {
			if region.End <= region.Start {
				t.Errorf("%q: region %d is empty at [%d,%d)", sample, index, region.Start, region.End)
			}
			if index > 0 && regions[index-1].End != region.Start {
				t.Errorf("%q: region %d starts at %d, want %d",
					sample, index, region.Start, regions[index-1].End)
			}
			rebuilt.WriteString(sample[region.Start:region.End])
		}
		if rebuilt.String() != sample {
			t.Errorf("%q: regions do not reconstruct the source", sample)
		}
	}
}

// Nothing in the pipeline reads a clock, a random source, or a shared counter,
// so the same input must classify and compress to the same bytes every call.
func TestClassificationAndCompressionAreDeterministic(t *testing.T) {
	for _, sample := range samples {
		firstRegions := ClassifyRegions(sample)
		firstWords := classify(sample)
		for attempt := 0; attempt < 3; attempt++ {
			if !sameRegions(ClassifyRegions(sample), firstRegions) {
				t.Errorf("%q: regions varied between calls", sample)
			}
			if !sameWords(classify(sample), firstWords) {
				t.Errorf("%q: words varied between calls", sample)
			}
		}
		for _, level := range LevelNames {
			want := compressAt(sample, level)
			for attempt := 0; attempt < 3; attempt++ {
				if got := compressAt(sample, level); got != want {
					t.Errorf("%q [%s]: got %q, want %q", sample, level, got, want)
				}
			}
		}
	}
}

func compressAt(text string, level Level) string {
	return CompressText(CompressRequest{
		Text:    text,
		Level:   level,
		Context: CompressContext{Role: RoleUser, Kind: KindText},
	}).Text
}

func sameRegions(got, want []Region) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameWords(got, want []ClassifiedWord) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Dropping words may only shorten a block, and a rising level may only shorten
// it further. Whitespace-only and empty input carry no word to drop, so they
// pass through byte-identical.
func TestCompressionNeverGrowsOutput(t *testing.T) {
	for _, sample := range samples {
		previous := len(sample)
		for _, level := range LevelNames {
			got := compressAt(sample, level)
			if len(got) > len(sample) {
				t.Errorf("%q [%s]: output is longer than the input", sample, level)
			}
			if len(got) > previous {
				t.Errorf("%q [%s]: output grew as the level rose", sample, level)
			}
			previous = len(got)
		}
	}
	for _, blank := range []string{"", "   \n ", "\n\n\t "} {
		for _, level := range LevelNames {
			if got := compressAt(blank, level); got != blank {
				t.Errorf("%q [%s]: blank input became %q", blank, level, got)
			}
		}
	}
}

// The whole point of protecting a region is that whatever it covers reaches
// upstream unchanged, so a payload that arrived parseable stays parseable.
func TestPrettyPrintedJSONStaysParseable(t *testing.T) {
	payload := `{
  "idempotency_key": "cart-7731-a",
  "status": "a pending order",
  "has_more": false,
  "items": [
    {
      "sku": "the-classic-mug",
      "qty": 2
    }
  ]
}`
	var want any
	if err := json.Unmarshal([]byte(payload), &want); err != nil {
		t.Fatalf("the fixture itself is not JSON: %v", err)
	}
	for _, level := range LevelNames {
		compressed := compressAt(payload, level)
		var got any
		if err := json.Unmarshal([]byte(compressed), &got); err != nil {
			t.Errorf("[%s]: compressed payload no longer parses: %v\n%s", level, err, compressed)
			continue
		}
		if ir := mustMarshal(t, got); ir != mustMarshal(t, want) {
			t.Errorf("[%s]: payload changed value\n got %s\nwant %s", level, ir, mustMarshal(t, want))
		}
	}
}

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	return string(encoded)
}

// Cutting between words must never cut inside a rune. Go strings tolerate
// invalid UTF-8 silently, so this is what catches an offset that landed mid
// sequence — the equivalent of the lone surrogate the oracle checked for.
func TestCompressionEmitsOnlyWellFormedUTF8(t *testing.T) {
	for _, sample := range samples {
		for _, level := range LevelNames {
			got := compressAt(sample, level)
			if !utf8.ValidString(got) {
				t.Errorf("%q [%s]: output is not valid UTF-8: %q", sample, level, got)
			}
			if first, _ := utf8.DecodeRuneInString(got); unicode.Is(unicode.Mn, first) {
				t.Errorf("%q [%s]: output opens with a stranded combining mark: %q",
					sample, level, got)
			}
		}
	}
}

// The gap a dropped word leaves must not swallow the structure around it: a
// paragraph break stays a paragraph break and a single newline stays single.
func TestLineBreaksSurviveADrop(t *testing.T) {
	cases := []struct {
		text  string
		level Level
		want  string
	}{
		{"the first thing here.\n\nthe second thing here.", LevelLight, "first thing here.\n\nsecond thing here."},
		{"the first thing here.\n\nthe second thing here.", LevelModerate, "first thing here.\n\nsecond thing here."},
		{"the first thing here.\n\nthe second thing here.", LevelCaveman, "first thing here.\n\nsecond thing here."},
		{"one line\nthe next line", LevelModerate, "one line\nnext line"},
	}
	for _, test := range cases {
		if got := compressAt(test.text, test.level); got != test.want {
			t.Errorf("%q [%s]: got %q, want %q", test.text, test.level, got, test.want)
		}
	}
}

// Adjacent drops must close up rather than leaving the run of spaces behind,
// since the spaces cost tokens the drop was meant to save.
func TestAdjacentDropsDoNotLeaveDoubleSpaces(t *testing.T) {
	const prose = "The compression proxy sits between a client and the API, so that callers " +
		"do not pay for tokens that contribute little to the output. It scores and " +
		"drops the lowest ranked tokens from eligible text, then forwards the request " +
		"upstream without any change to the client beyond one header."
	for _, level := range LevelNames {
		got := compressAt(prose, level)
		if strings.Contains(got, "  ") {
			t.Errorf("[%s]: output holds a run of spaces: %q", level, got)
		}
		if len(got) >= len(prose) {
			t.Errorf("[%s]: nothing was dropped from ordinary prose", level)
		}
		if strings.TrimSpace(got) == "" {
			t.Errorf("[%s]: prose carrying nouns and verbs was emptied", level)
		}
	}
}

// Terminal punctuation belongs to the sentence, not to the word it followed, so
// dropping that word must leave the sentence boundaries countable.
func TestSentencePunctuationSurvives(t *testing.T) {
	if got := compressAt("The dog sat on the mat.", LevelModerate); !strings.HasSuffix(got, ".") {
		t.Errorf("terminal period was dropped: %q", got)
	}
	got := compressAt("The man went to the store. The dog sat on the mat.", LevelModerate)
	if strings.Count(got, ".") != 2 {
		t.Errorf("sentence boundaries were lost: %q", got)
	}
}

// A word that is both content and removable-looking must be tagged by its role
// in the sentence, so both occurrences survive rather than one being read as a
// class the level drops.
func TestBothSensesOfBookSurvive(t *testing.T) {
	const text = "I need you to book a flight, and the book is on the table."
	got := compressAt(text, LevelCaveman)
	if count := strings.Count(got, "book"); count != 2 {
		t.Errorf("%d occurrences of \"book\" survived, want 2: %q", count, got)
	}
}

// Removing a subordinator leaves both clauses standing as separate assertions,
// which is a different claim from the one the original made.
func TestSubordinatorsAndNegationsSurviveEveryLevel(t *testing.T) {
	cases := []struct{ text, keyword string }{
		{"Do not proceed if the tests fail.", "if"},
		{"Retry the request unless the error is fatal.", "unless"},
		{"Stop the process when the queue is empty.", "when"},
		{"Run the linter before you commit the change.", "before"},
		{"Delete the branch after the pull request merges.", "after"},
		{"Do not deploy until the review is approved.", "until"},
		{"Skip the file because it is generated.", "because"},
		{"Fail the build although the warnings are minor.", "although"},
		{"Write to the log whenever a request is dropped.", "whenever"},
		{"Ask the user first, otherwise assume the default.", "otherwise"},
		{"Use the cache while the connection is alive.", "while"},
		{"Escalate except when the caller is an admin.", "except"},
		{"The change did not break the very large test suite.", "not"},
		{"Only use emojis if the user explicitly asks.", "if"},
		{"If the tests fail, do not proceed.", "If"},
		{"it doesn't work", "doesn't"},
		{"i can't reproduce it", "can't"},
		{"that won't happen", "won't"},
	}
	for _, test := range cases {
		for _, level := range LevelNames {
			if got := compressAt(test.text, level); !strings.Contains(got, test.keyword) {
				t.Errorf("%q [%s]: %q was dropped, got %q", test.text, level, test.keyword, got)
			}
		}
	}
}

// A participle carrying its clause is the predicate, not decoration, so caveman
// must keep it — and must still drop it where the clause has a verb of its own.
func TestPredicateParticiplesSurviveButAdjectivalOnesDoNot(t *testing.T) {
	for _, text := range []string{"50 requests abandoned", "the build failed", "job cancelled"} {
		for _, level := range LevelNames {
			got := compressAt(text, level)
			last := text[strings.LastIndex(text, " ")+1:]
			if !strings.HasSuffix(got, last) {
				t.Errorf("%q [%s]: the predicate was dropped, got %q", text, level, got)
			}
		}
	}
	if got := compressAt("an abandoned building was sold", LevelCaveman); got != "building sold" {
		t.Errorf("got %q, want %q", got, "building sold")
	}
}

// Content classes are never removable, so a sentence built from them alone
// comes back whole at every level.
func TestContentWordsSurviveEveryLevel(t *testing.T) {
	const text = "John quickly sent the 42 large reports to Paris on Tuesday."
	for _, level := range LevelNames {
		got := compressAt(text, level)
		for _, word := range []string{"John", "Paris", "42", "reports"} {
			if !strings.Contains(got, word) {
				t.Errorf("[%s]: %q was dropped, got %q", level, word, got)
			}
		}
	}
}

// The level design is stated in terms of which classes each one adds, so these
// pin the boundaries: a preposition survives light and not moderate, and an
// adjective survives moderate and not caveman.
func TestLevelBoundariesAddTheClassesTheyName(t *testing.T) {
	const withPreposition = "She went to the store with him."
	if got := compressAt(withPreposition, LevelLight); !strings.Contains(got, "to") {
		t.Errorf("light dropped a preposition: %q", got)
	}
	if got := compressAt(withPreposition, LevelModerate); strings.Contains(got, " to ") {
		t.Errorf("moderate kept a preposition: %q", got)
	}

	const withModifiers = "The very large dog quickly ate the food."
	moderate := compressAt(withModifiers, LevelModerate)
	if !strings.Contains(moderate, "large") || !strings.Contains(moderate, "quickly") {
		t.Errorf("moderate dropped a modifier: %q", moderate)
	}
	caveman := compressAt(withModifiers, LevelCaveman)
	if strings.Contains(caveman, "large") || strings.Contains(caveman, "quickly") {
		t.Errorf("caveman kept a modifier: %q", caveman)
	}
	for _, level := range LevelNames {
		if got := compressAt("The man went to the store.", level); strings.Contains(got, " the ") {
			t.Errorf("[%s]: a determiner survived: %q", level, got)
		}
	}
}

// A block of nothing but removable words compresses to nothing. What the
// pipeline does with that is its own concern; here the compressor reports it.
func TestABlockOfOnlyRemovableWordsEmpties(t *testing.T) {
	if got := compressAt("to the", LevelModerate); got != "" {
		t.Errorf("got %q, want the empty string", got)
	}
	if got := compressAt("the the the", LevelLight); got != "" {
		t.Errorf("got %q, want the empty string", got)
	}
}

// A block that is entirely protected has no prose to drop, so it is returned
// byte-identical rather than reassembled.
func TestAFullyProtectedBlockIsReturnedWhole(t *testing.T) {
	const fenced = "```ts\nconst value = compute(alpha, beta);\nreturn value;\n```"
	for _, level := range LevelNames {
		if got := compressAt(fenced, level); got != fenced {
			t.Errorf("[%s]: got %q, want the block unchanged", level, got)
		}
	}
	if words := classify(fenced); len(words) != 0 {
		t.Errorf("classified %d words inside a fully protected block", len(words))
	}
}

// No word may be classified inside a protected region, or compression would be
// free to drop something the region exists to keep.
func TestNoWordIsClassifiedInsideAProtectedRegion(t *testing.T) {
	for _, sample := range samples {
		regions := ClassifyRegions(sample)
		for _, word := range ClassifyWords(sample, regions) {
			for _, region := range regions {
				if region.Kind != RegionProtected {
					continue
				}
				if word.Start < region.End && word.End > region.Start {
					t.Errorf("%q: word %q overlaps protected [%d,%d)",
						sample, word.Text, region.Start, region.End)
				}
			}
		}
	}
}

// Ordinary prose carries no structure to protect, so it must come back as one
// prose region rather than as a tiling with a spurious protected span in it.
func TestOrdinaryProseIsOneProseRegion(t *testing.T) {
	const text = "The man went to the store and bought some bread."
	regions := ClassifyRegions(text)
	if len(regions) != 1 || regions[0] != (Region{Kind: RegionProse, Start: 0, End: len(text)}) {
		t.Errorf("got %v, want one prose region covering the text", regions)
	}
}

// Each of these is a shape the oracle protected. A pattern that stopped
// matching one would not show up in the golden corpus if the corpus never
// carries that shape, so they are pinned by name.
func TestProtectedShapes(t *testing.T) {
	cases := []struct{ name, text, needle string }{
		{"a fenced code block", "```ts\ncode()\n```", "code()"},
		{"a tilde fence", "~~~\ncode()\n~~~", "code()"},
		{"an indented code block", "prose\n\n    indented()\n", "indented()"},
		{"inline code", "the `value` here", "`value`"},
		{"a URL with a query string", "see https://x.example/a?b=1&c=2 now", "https://x.example/a?b=1&c=2"},
		{"a file path", "open src/compression/compress.ts now", "src/compression/compress.ts"},
		{"a relative path", "see ./a/b/c.txt here", "./a/b/c.txt"},
		{"a windows path", `open C:\Users\me\a.txt now`, `C:\Users\me\a.txt`},
		{"a JSON object", `body {"a": 1} sent`, `{"a": 1}`},
		{"a JSX element", `the <Foo bar="x" /> node`, `<Foo bar="x" />`},
		{"a stack frame", "at Foo.bar (app.js:10:5) failed", "(app.js:10:5)"},
		{"a python trace line", `File "x.py", line 3`, `File "x.py"`},
		{"a hex literal", "mask 0xDEADBEEF set", "0xDEADBEEF"},
		{"a UUID", "id 550e8400-e29b-41d4-a716-446655440000 here", "550e8400-e29b-41d4-a716-446655440000"},
		{"a long digit run", "ref 1234567890 seen", "1234567890"},
		{"a version string", "needs ^14.16.0 exactly", "^14.16.0"},
		{"a table row", "| a | b |\n| - | - |", "| a | b |"},
		{"a list bullet", "- an item here", "-"},
		{"a header marker", "# A Header", "#"},
		{"a block quote marker", "> quoted text", ">"},
		{"a snake_case identifier", "the user_id field", "user_id"},
		{"a function call", "call compute(a, b) now", "compute(a, b)"},
		{"a pretty-printed object line", "{\n  \"idempotency_key\": \"cart-7731-a\",\n}", `"idempotency_key": "cart-7731-a",`},
		{"a string value holding words", "{\n  \"status\": \"a pending order\",\n}", `"status": "a pending order",`},
		{"a boolean value", "{\n  \"has_more\": false\n}", `"has_more": false`},
		{"a quoted string in prose", `she said "the very large dog" loudly`, `"the very large dog"`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if !isProtected(test.text, test.needle) {
				t.Errorf("%q was not protected in %q", test.needle, test.text)
			}
		})
	}

	// A fence protects its whole body, so patterns that would fire inside it
	// never get the chance to carve it up.
	const fenced = "before\n```\nhttps://x.io/a?b=1\n| a | b |\n```\nafter"
	for _, needle := range []string{"https://x.io/a?b=1", "| a | b |"} {
		if !isProtected(fenced, needle) {
			t.Errorf("%q inside a fence was not protected", needle)
		}
	}

	// The marker is structure; the text after it is prose and stays compressible.
	for _, test := range []struct{ text, prose string }{
		{"- The first item here", "The first item here"},
		{"# The Header", "The Header"},
	} {
		if !strings.Contains(proseOf(test.text), test.prose) {
			t.Errorf("%q left no compressible prose", test.text)
		}
	}
}

func isProtected(text, needle string) bool {
	for _, region := range ClassifyRegions(text) {
		if region.Kind == RegionProtected && strings.Contains(text[region.Start:region.End], needle) {
			return true
		}
	}
	return false
}

func proseOf(text string) string {
	out := strings.Builder{}
	for _, region := range ClassifyRegions(text) {
		if region.Kind == RegionProse {
			out.WriteString(text[region.Start:region.End])
		}
	}
	return out.String()
}

// Each of these shapes must reach upstream byte-identical after compression,
// not merely be protected at classification time.
func TestProtectedShapesSurviveCompressionVerbatim(t *testing.T) {
	cases := []struct{ name, text, kept string }{
		{"a fenced code block",
			"Here is the code:\n```ts\nconst a = the(b, c);\n```\nand the rest.",
			"```ts\nconst a = the(b, c);\n```"},
		{"unfenced JSON",
			`The payload is {"user_id": 4, "the": 1} in the body.`,
			`{"user_id": 4, "the": 1}`},
		{"a stack trace line",
			"It failed:\n    at Foo.bar (app.js:10:5)\nand the cause is unclear.",
			"at Foo.bar (app.js:10:5)"},
		{"a URL with a query string",
			"Fetch https://api.example.com/v1/x?y=1&z=2 from the server.",
			"https://api.example.com/v1/x?y=1&z=2"},
		{"a file path",
			"Open the file src/compression/compress.ts in the editor.",
			"src/compression/compress.ts"},
		{"a UUID",
			"The record id is 550e8400-e29b-41d4-a716-446655440000 in the table.",
			"550e8400-e29b-41d4-a716-446655440000"},
		{"a markdown table row",
			"| the col | the other |\n| --- | --- |\nThe table is above.",
			"| the col | the other |\n| --- | --- |"},
		{"a hex literal", "The mask is 0xDEADBEEF in the register.", "0xDEADBEEF"},
		{"a version string", "It needs the version ^14.16.0 from the registry.", "^14.16.0"},
		{"a JSX element",
			`Render the <Component prop="the value" /> in the tree.`,
			`<Component prop="the value" />`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, level := range LevelNames {
				if got := compressAt(test.text, level); !strings.Contains(got, test.kept) {
					t.Errorf("[%s]: %q missing from %q", level, test.kept, got)
				}
			}
		})
	}
}

// An emoji sequence that survives must survive whole: half a ZWJ sequence is a
// different glyph, and a flag reduced to one regional indicator is a letter.
func TestSurvivingEmojiSequencesStayWhole(t *testing.T) {
	const family = "\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466"
	const text = "keep " + family + " the family emoji intact when compressing this text"
	for _, level := range LevelNames {
		got := compressAt(text, level)
		if strings.Contains(got, "\U0001F468") && !strings.Contains(got, family) {
			t.Errorf("[%s]: the family sequence was split: %q", level, got)
		}
	}
	// The gap a dropped word leaves is where the emoji sits. Treating that gap
	// as stranded punctuation would take the emoji with it.
	for _, level := range LevelNames {
		if got := compressAt("The "+family+" family is here.", level); !strings.Contains(got, family) {
			t.Errorf("[%s]: an emoji beside a dropped word was lost: %q", level, got)
		}
		if got := compressAt("\U0001F1EF\U0001F1F5 is the flag.", level); !strings.Contains(got, "\U0001F1EF\U0001F1F5") {
			t.Errorf("[%s]: a leading flag was lost: %q", level, got)
		}
	}
}

// isLevel is what rejects a header value before it reaches the compressor, so
// the names it accepts are the names the levels are defined under.
func TestLevelNamesAreExactlyTheThree(t *testing.T) {
	for _, level := range LevelNames {
		if !policy.IsLevel(string(level)) {
			t.Errorf("%q is a level name but was rejected", level)
		}
	}
	for _, value := range []string{"0.5", "heuristic", "", "LIGHT", "off"} {
		if policy.IsLevel(value) {
			t.Errorf("%q was accepted as a level name", value)
		}
	}
}

// Light removes determiners and nothing else, and no level removes a content
// class. Together those are what make the levels a nested sequence rather than
// three independent rule sets.
func TestRemovableSetsAreNestedAndSpareContentClasses(t *testing.T) {
	if len(removable[LevelLight]) != 1 {
		t.Errorf("light removes %d classes, want 1", len(removable[LevelLight]))
	}
	if _, ok := removable[LevelLight][ClassDeterminer]; !ok {
		t.Error("light does not remove determiners")
	}
	if len(removable[LevelLight]) >= len(removable[LevelModerate]) ||
		len(removable[LevelModerate]) >= len(removable[LevelCaveman]) {
		t.Errorf("the removable sets do not grow strictly: %d, %d, %d",
			len(removable[LevelLight]), len(removable[LevelModerate]), len(removable[LevelCaveman]))
	}
	content := []WordClass{ClassNoun, ClassVerb, ClassNumber, ClassProper, ClassPredicate, ClassOther}
	for _, level := range LevelNames {
		for _, class := range content {
			if IsRemovable(level, class) {
				t.Errorf("%s removes %s, a content class", level, class)
			}
		}
	}
}
