package compress

import (
	"strings"
	"unicode"

	"github.com/carlelieser/caveman/internal/tagger"
)

// The gap between two units is whatever the source held there: whitespace, the
// punctuation the dropped words carried, or both. Rebuilding a gap is what
// decides whether two surviving words read as one sentence or two, so it is the
// only place in the pipeline that rewrites bytes it did not copy.

// isJSSpace matches JavaScript's `\s`, which is wider than Go's: it includes the
// Unicode space separators and the two line terminators as well as the ASCII set.
// The gap arithmetic reads whitespace straight out of the source, so a narrower
// class here would leave a non-breaking space standing where the TS collapsed it.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// isPictographic is Extended_Pictographic, which is the union table the tagger
// carries minus the 31 codepoints that hold Emoji_Presentation without it: the
// 26 regional indicators and the 5 skin-tone modifiers. Those are `\p{S}`, so
// including them would keep a leading flag the TS strips.
func isPictographic(r rune) bool {
	if (r >= 0x1f1e6 && r <= 0x1f1ff) || (r >= 0x1f3fb && r <= 0x1f3ff) {
		return false
	}
	return tagger.IsPictographic(r)
}

// isOrphan matches one character of `^(?:(?!\p{Extended_Pictographic})[\p{P}\p{S}\s])`.
// Pictographs are excluded even though they are symbols: an emoji carries meaning
// on its own, so a word dropped beside one must not take it along.
func isOrphan(r rune) bool {
	if isPictographic(r) {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r) || isJSSpace(r)
}

// stripLeadingPunctuation removes the punctuation and whitespace a dropped word
// left behind, up to the first character that carries meaning of its own.
func stripLeadingPunctuation(gap string) string {
	for index, r := range gap {
		if !isOrphan(r) {
			return gap[index:]
		}
	}
	return ""
}

// terminalMarkOf finds sentence-final punctuation, kept even when the word it was
// attached to is dropped. A compressed block runs several sentences together
// without it, and the reader — the model — loses the boundaries entirely.
func terminalMarkOf(gap string) string {
	for index, r := range gap {
		if !strings.ContainsRune(".!?;:,", r) {
			continue
		}
		rest := gap[index+len(string(r)):]
		if rest == "" {
			return string(r)
		}
		for _, next := range rest {
			if isJSSpace(next) {
				return string(r)
			}
			break
		}
	}
	return ""
}

// collapseWhitespace reduces each whitespace run to a space, a line break, or a
// blank line.
func collapseWhitespace(gap string) string {
	out := strings.Builder{}
	runStart := -1
	for index, r := range gap {
		if isJSSpace(r) {
			if runStart == -1 {
				runStart = index
			}
			continue
		}
		if runStart != -1 {
			out.WriteString(breakFor(gap[runStart:index]))
			runStart = -1
		}
		out.WriteRune(r)
	}
	if runStart != -1 {
		out.WriteString(breakFor(gap[runStart:]))
	}
	return out.String()
}

func breakFor(run string) string {
	switch strings.Count(run, "\n") {
	case 0:
		return " "
	case 1:
		return "\n"
	default:
		return "\n\n"
	}
}

func stripOrphans(gap string) string {
	stripped := stripLeadingPunctuation(gap)
	if stripped == "" {
		return breakFor(gap)
	}
	return stripped
}

// joinGap folds a gap accumulated across dropped words. It holds the punctuation
// those words were attached to; keeping it would strand commas and periods
// against unrelated words, so only the separator itself survives — unless the gap
// ends a sentence, in which case that mark is put back in front of the separator.
func joinGap(gap string) string {
	stripped := collapseWhitespace(stripOrphans(gap))
	return terminalMarkOf(gap) + stripped
}
