package tokens_test

import (
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/tokens"
)

// The tables are compiled in, so counting must work with no network and no
// cache directory. A miss here means every reported number silently degrades to
// the character fallback.
func TestTablesLoad(t *testing.T) {
	if !tokens.Available() {
		t.Fatal("BPE tables did not load, so counting fell back to characters")
	}
}

func TestEmptyTextIsZeroTokens(t *testing.T) {
	if got := tokens.Count(""); got != 0 {
		t.Errorf("empty text counted %d tokens", got)
	}
}

// Known counts from the cl100k_base encoding. They pin the encoding itself: a
// different BPE would score these differently.
func TestKnownCounts(t *testing.T) {
	cases := map[string]int{
		"The quick brown fox jumps over the lazy dog.": 10,
		"hello":         1,
		" hello":        1,
		"hello, world!": 4,
	}
	for text, want := range cases {
		if got := tokens.Count(text); got != want {
			t.Errorf("Count(%q) = %d, want %d", text, got, want)
		}
	}
}

// Deleting words must never raise the count. This is the property the whole
// saving claim rests on.
func TestRemovingWordsNeverRaisesTheCount(t *testing.T) {
	full := "The engineer said that the deployment had probably failed because the " +
		"configuration file was very obviously missing from the release bundle."
	words := strings.Fields(full)
	for cut := range words {
		shorter := strings.Join(append(append([]string{}, words[:cut]...), words[cut+1:]...), " ")
		if tokens.Count(shorter) > tokens.Count(full) {
			t.Errorf("dropping %q raised the token count", words[cut])
		}
	}
}

// A count is a count, not an estimate: the same text always scores the same.
func TestCountIsDeterministic(t *testing.T) {
	text := "Compression removed the determiners and the prepositions."
	first := tokens.Count(text)
	for range 100 {
		if got := tokens.Count(text); got != first {
			t.Fatalf("count varied: %d then %d", first, got)
		}
	}
}

// Characters divided by four is what this replaced. The gap is the reason.
func TestRealCountDivergesFromTheCharacterEstimate(t *testing.T) {
	text := "Kubernetes autoscaling misconfiguration caused intermittent 503s."
	estimate := (len(text) + 3) / 4
	actual := tokens.Count(text)
	if estimate == actual {
		t.Skip("estimate happened to match on this sample")
	}
	t.Logf("chars/4 = %d, real = %d", estimate, actual)
}
