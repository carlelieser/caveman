// Package tokens counts tokens the way a provider's tokenizer does, rather than
// estimating from character counts.
//
// Anthropic does not publish Claude's tokenizer, so the closest available BPE
// is used: cl100k_base, the encoding OpenAI ships for GPT-4-class models. It
// segments text on the same principles Claude's does — subword merges over
// UTF-8 bytes, whole common words as single tokens, whitespace bound to the
// word that follows — so a count from it tracks a billed count far more closely
// than characters divided by a constant, which tracks nothing. It is still an
// approximation, and the exact billed numbers ride back in the upstream
// response's usage (see usage.go).
package tokens

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	loader "github.com/pkoukk/tiktoken-go-loader"
)

// encodingName is the BPE Caveman counts with.
const encodingName = "cl100k_base"

// fallbackCharsPerToken is used only when the BPE tables fail to load, which
// leaves counting degraded rather than broken. Reported counts say which of the
// two produced them, so a degraded number is never mistaken for a real one.
const fallbackCharsPerToken = 4

var (
	once     sync.Once
	encoding *tiktoken.Tiktoken
)

// load reads the BPE tables once. The loader is the offline one, so the tables
// are compiled in and no request ever waits on a download.
func load() {
	tiktoken.SetBpeLoader(loader.NewOfflineLoader())
	if built, err := tiktoken.GetEncoding(encodingName); err == nil {
		encoding = built
	}
}

// Available reports whether real tokenization is in use. False means the tables
// did not load and Count is falling back to characters.
func Available() bool {
	once.Do(load)
	return encoding != nil
}

// Count is the number of tokens in text.
func Count(text string) int {
	once.Do(load)
	if encoding == nil {
		return fallbackCount(text)
	}
	return len(encoding.Encode(text, nil, nil))
}

func fallbackCount(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + fallbackCharsPerToken - 1) / fallbackCharsPerToken
}
