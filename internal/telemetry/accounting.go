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
}

type TokenAccounting struct {
	TokensBefore int
	TokensAfter  int
	TokensSaved  int
	Ratio        float64
	Level        policy.Level
}

const (
	HeaderTokensBefore = "X-Caveman-Tokens-Before"
	HeaderTokensAfter  = "X-Caveman-Tokens-After"
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
	tokensBefore := stats.TokensBefore
	tokensAfter := stats.TokensAfter
	return TokenAccounting{
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		TokensSaved:  tokensBefore - tokensAfter,
		Ratio:        roundRatio(reductionRatio(tokensBefore, tokensAfter)),
		Level:        stats.Level,
	}
}

// ApplyAccountingHeaders labels a response with what compression did. Stats are
// attached even when a request fails upstream, so a compression-induced 4xx
// stays attributable to the ratio that caused it.
func ApplyAccountingHeaders(headers http.Header, accounting TokenAccounting) {
	headers.Set(HeaderTokensBefore, strconv.Itoa(accounting.TokensBefore))
	headers.Set(HeaderTokensAfter, strconv.Itoa(accounting.TokensAfter))
	headers.Set(HeaderRatio, strconv.FormatFloat(accounting.Ratio, 'f', 4, 64))
	headers.Set(HeaderLevel, string(accounting.Level))
}
