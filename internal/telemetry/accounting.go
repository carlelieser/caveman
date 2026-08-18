package telemetry

import (
	"math"
	"net/http"
	"strconv"

	"github.com/carlelieser/caveman/internal/policy"
)

// PipelineStats is what one compression pass did. The compression pipeline
// fills it in; accounting only reads it.
type PipelineStats struct {
	Level           policy.Level
	NodesSeen       int
	NodesCompressed int
	// NodesSkipped counts in-scope nodes left untouched to keep a cached prefix
	// byte-stable.
	NodesSkipped int
	CharsBefore  int
	CharsAfter   int
	// CharsProse counts prose characters across every node seen, skipped ones
	// included.
	CharsProse int
	// TokensBefore and TokensAfter are real tokenizer counts, summed per node
	// as the pipeline walks. They are counted there and not derived here
	// because tokenization needs the text, which a character count has already
	// thrown away: tokens do not divide across a concatenation boundary the way
	// characters do, so a whole-request count and the sum of its nodes differ
	// slightly. Summing per node is what makes before and after comparable,
	// since both are summed over the same node boundaries.
	TokensBefore int
	TokensAfter  int
	// Counted records whether a tokenizer actually ran. Without it a zero token
	// total is ambiguous: it could mean counting was off, or an empty request.
	Counted bool
}

// TokenAccounting is one request's saving, in whichever unit was measured.
// Counted records which: true means the token fields came from a real tokenizer
// pass, false means counting was off and only the character fields are real.
// Before/After/Saved carry the measured unit so readers need not branch.
type TokenAccounting struct {
	TokensBefore int
	TokensAfter  int
	CharsBefore  int
	CharsAfter   int
	Before       int
	After        int
	Saved        int
	Ratio        float64
	Counted      bool
	Level        policy.Level
}

// Unit names what Before, After and Saved are counted in, for reports that
// print the figure next to its unit.
func (a TokenAccounting) Unit() string {
	if a.Counted {
		return "tok"
	}
	return "char"
}

const (
	HeaderTokensBefore = "X-Caveman-Tokens-Before"
	HeaderTokensAfter  = "X-Caveman-Tokens-After"
	HeaderCharsBefore  = "X-Caveman-Chars-Before"
	HeaderCharsAfter   = "X-Caveman-Chars-After"
	HeaderRatio        = "X-Caveman-Ratio"
	HeaderLevel        = "X-Caveman-Level"
)

// reductionRatio is the fraction of tokens removed. Zero when there was
// nothing to drop.
func reductionRatio(tokensBefore, tokensAfter int) float64 {
	if tokensBefore == 0 {
		return 0
	}
	return float64(tokensBefore-tokensAfter) / float64(tokensBefore)
}

func roundRatio(ratio float64) float64 {
	return math.Round(ratio*10000) / 10000
}

func AccountFor(stats PipelineStats) TokenAccounting {
	// Counting is opt-in, so token totals are zero when it was off. The
	// character totals are always real, and both sides measure the same deleted
	// words, so the ratio means the same thing either way.
	counted := stats.Counted
	before, after := stats.CharsBefore, stats.CharsAfter
	if counted {
		before, after = stats.TokensBefore, stats.TokensAfter
	}
	return TokenAccounting{
		TokensBefore: stats.TokensBefore,
		TokensAfter:  stats.TokensAfter,
		CharsBefore:  stats.CharsBefore,
		CharsAfter:   stats.CharsAfter,
		Before:       before,
		After:        after,
		Saved:        before - after,
		Ratio:        roundRatio(reductionRatio(before, after)),
		Counted:      counted,
		Level:        stats.Level,
	}
}

// ApplyAccountingHeaders labels a response with what compression did. Stats are
// attached even when a request fails upstream, so a compression-induced 4xx
// stays attributable to the ratio that caused it.
func ApplyAccountingHeaders(headers http.Header, accounting TokenAccounting) {
	// Token headers appear only when a tokenizer produced them, so a zero is
	// never mistaken for a measured count. Character headers are always real.
	if accounting.Counted {
		headers.Set(HeaderTokensBefore, strconv.Itoa(accounting.TokensBefore))
		headers.Set(HeaderTokensAfter, strconv.Itoa(accounting.TokensAfter))
	}
	headers.Set(HeaderCharsBefore, strconv.Itoa(accounting.CharsBefore))
	headers.Set(HeaderCharsAfter, strconv.Itoa(accounting.CharsAfter))
	headers.Set(HeaderRatio, strconv.FormatFloat(accounting.Ratio, 'f', 4, 64))
	headers.Set(HeaderLevel, string(accounting.Level))
}
