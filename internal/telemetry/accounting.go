package telemetry

import (
	"math"
	"net/http"
	"strconv"

	"github.com/carlelieser/caveman/internal/policy"
)

// Characters per token for English-weighted prose. Estimation is local because
// an upstream count_tokens call per request would cost more than the saving it
// measures; the billed counts arrive in the upstream response's usage.
const charsPerToken = 4

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

func estimateTokens(charCount int) int {
	return int(math.Ceil(float64(charCount) / charsPerToken))
}

// reductionRatio is the fraction of estimated tokens removed. Zero when there
// was nothing to drop.
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
	tokensBefore := estimateTokens(stats.CharsBefore)
	tokensAfter := estimateTokens(stats.CharsAfter)
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
