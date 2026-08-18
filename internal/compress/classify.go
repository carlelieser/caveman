package compress

import (
	"strings"

	"github.com/rivo/uniseg"

	"github.com/carlelieser/caveman/internal/tagger"
)

type WordClass string

const (
	ClassDeterminer  WordClass = "determiner"
	ClassPreposition WordClass = "preposition"
	ClassConjunction WordClass = "conjunction"
	ClassAuxiliary   WordClass = "auxiliary"
	ClassCopula      WordClass = "copula"
	ClassPronoun     WordClass = "pronoun"
	ClassAdverb      WordClass = "adverb"
	ClassAdjective   WordClass = "adjective"
	// ClassPredicate is an adjective carrying its clause's assertion, as in `connection refused`.
	ClassPredicate WordClass = "predicate"
	ClassNoun      WordClass = "noun"
	ClassVerb      WordClass = "verb"
	ClassNumber    WordClass = "number"
	ClassProper    WordClass = "proper"
	ClassOther     WordClass = "other"
)

// ClassifiedWord carries byte offsets into the source string.
type ClassifiedWord struct {
	Text      string
	Start     int
	End       int
	WordClass WordClass
}

// Region marks a span of the source. Only prose regions are classified.
type Region struct {
	Kind  string
	Start int
	End   int
}

const (
	verbTag      = "Verb"
	adjectiveTag = "Adjective"
	// predicateTag is added by markPredicates; compromise never emits it.
	predicateTag = "CavemanPredicate"
	// subordinatorTag is added by markSubordinators; compromise never emits it.
	subordinatorTag = "CavemanSubordinator"
)

// subordinators are words that subordinate a clause: they mark it as a condition,
// a time bound, a cause, a concession, an exception, or an alternative. Dropping
// one shortens the sentence and changes what it claims. "Do not proceed if the
// tests fail" becomes "do not proceed, the tests fail", a claim that the tests
// failed. The saving is real; the damage is invisible beside it.
//
// This is a lexical list rather than a tag rule because compromise's tags do not
// separate the two uses. It tags `before` as `Conjunction` in both "proceed before
// the tests pass" and "the file before the directory", and it scatters the rest of
// the class across `Conjunction`, `Preposition`, `Adverb` and `Determiner`. Only
// `unless` and `lest` get their own `Condition` tag, which is why those two
// survived removal while the rest of the class did not.
//
// The cost of keeping a non-subordinating use — `after` in "the name comes after
// the colon" — is one short function word. The cost of dropping a subordinating
// one is the meaning of the clause, so the ambiguous case resolves to keeping, as
// it does everywhere else in the pipeline.
var subordinators = map[string]struct{}{
	"if": {}, "unless": {}, "lest": {}, "whether": {}, "when": {},
	"whenever": {}, "while": {}, "until": {}, "till": {}, "before": {},
	"after": {}, "once": {}, "since": {}, "because": {}, "although": {},
	"though": {}, "whereas": {}, "otherwise": {}, "except": {},
	"despite": {}, "unlike": {},
}

// tagPriority maps compromise's tags in priority order. Order is what makes the
// mapping well-defined: its tags co-occur freely — a pronoun also carries `Noun`,
// a copula also carries `Verb` and often `Auxiliary` — so the first match wins and
// the more specific tag has to come first.
//
// `Negative` is listed above everything it co-occurs with because dropping `not`
// inverts the sentence. It resolves to `other`, which no level removes.
var tagPriority = []struct {
	tag   string
	class WordClass
}{
	{"Negative", ClassOther},
	{subordinatorTag, ClassOther},
	{"Condition", ClassOther},
	{"QuestionWord", ClassOther},
	{"Expression", ClassOther},
	{"Emoji", ClassOther},
	{"Acronym", ClassOther},
	{"Abbreviation", ClassOther},
	{"ProperNoun", ClassProper},
	{"Value", ClassNumber},
	{"Pronoun", ClassPronoun},
	{"Determiner", ClassDeterminer},
	{"Copula", ClassCopula},
	{"Auxiliary", ClassAuxiliary},
	{"Modal", ClassAuxiliary},
	{"Preposition", ClassPreposition},
	{"Conjunction", ClassConjunction},
	{"Adverb", ClassAdverb},
	{predicateTag, ClassPredicate},
	{adjectiveTag, ClassAdjective},
	{verbTag, ClassVerb},
	{"Noun", ClassNoun},
}

// offsetSlack is how far past the cursor a term's text may be found. Compromise
// reports the separator it consumed in `pre`, so the text should sit immediately
// after it; the slack absorbs a separator it rewrote (an unspaced em-dash becomes
// a hyphen) without letting a repeat of the same word further along be claimed.
//
// The TS measures this in UTF-16 code units; here it is bytes. Both are a
// tolerance on a search that must still land on an exact match, so the wider byte
// window cannot admit a location the exact-match test would reject.
const offsetSlack = 8

type term struct {
	text string
	pre  string
	post string
	tags []string
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// classOf reads tags as a plain slice in the order compromise emits them, never
// through a set, so nothing here depends on iteration order.
func classOf(tags []string) WordClass {
	for _, entry := range tagPriority {
		if hasTag(tags, entry.tag) {
			return entry.class
		}
	}
	return ClassOther
}

// mergeTaglessTerms folds a contraction's expansion back into the word that
// produced it. Compromise splits a contraction into one term per part it expands
// to, and only the first carries text: `doesn't` is an `Auxiliary` term followed
// by a text-less `Negative` one. A term with no text has no offset in the source,
// so its tags belong to the word that produced it.
func mergeTaglessTerms(terms []term) []term {
	merged := make([]term, 0, len(terms))
	for _, t := range terms {
		if t.text == "" && len(merged) > 0 {
			prev := &merged[len(merged)-1]
			prev.post = prev.post + t.pre + t.post
			prev.tags = append(append([]string{}, prev.tags...), t.tags...)
			continue
		}
		merged = append(merged, t)
	}
	return merged
}

// markPredicates promotes an adjective that is holding a clause's assertion.
// Compromise tags a past participle `Adjective`, which is right for `an abandoned
// building` and wrong for `50 requests abandoned`, where the participle is the
// whole predication and the copula has been left out. A sentence carrying no verb
// has nothing else to predicate with, so an adjective in one is holding the
// assertion rather than describing a noun.
func markPredicates(terms []term) []term {
	for _, t := range terms {
		if hasTag(t.tags, verbTag) {
			return terms
		}
	}
	out := make([]term, len(terms))
	copy(out, terms)
	for i := range out {
		if hasTag(out[i].tags, adjectiveTag) && followsANoun(out, i) {
			out[i].tags = append(append([]string{}, out[i].tags...), predicateTag)
		}
	}
	return out
}

// followsANoun separates an attributive adjective from a predicative one: the
// first precedes what it describes and the second follows its subject, which is
// all that tells `a very large dog` from `connection refused` once neither has a
// verb.
func followsANoun(terms []term, index int) bool {
	for _, t := range terms[:index] {
		if classOf(t.tags) == ClassNoun {
			return true
		}
	}
	return false
}

// markSubordinators tags a term whose text is a subordinator so it resolves to
// `other`, which no level removes. Matching is on the lowercased text because the
// class is closed and its members are spelled one way; a sentence-initial `If` is
// the same word as a medial `if`.
func markSubordinators(terms []term) []term {
	for i := range terms {
		if _, ok := subordinators[strings.ToLower(terms[i].text)]; ok {
			terms[i].tags = append(append([]string{}, terms[i].tags...), subordinatorTag)
		}
	}
	return terms
}

func parseTerms(text string) []term {
	doc := tagger.Parse(text)
	out := []term{}
	for _, sentence := range doc {
		terms := make([]term, 0, len(sentence))
		for _, t := range sentence {
			terms = append(terms, term{text: t.Text, pre: t.Pre, post: t.Post, tags: t.TagList()})
		}
		out = append(out, markSubordinators(markPredicates(mergeTaglessTerms(terms)))...)
	}
	return out
}

// locate finds a term's text in the original string at or after the cursor.
//
// Compromise normalizes as it parses — it collapses non-breaking spaces and
// rewrites some dashes — so neither its own offsets nor `pre` length arithmetic
// can be trusted to land on the right character. Only an exact match of the term
// text counts as a location; anything else returns -1 and the word is left
// unclassified rather than sliced at a guessed offset.
func locate(text string, t term, cursor int) int {
	limit := cursor + len(t.pre) + len(t.text) + offsetSlack
	if cursor > len(text) {
		return -1
	}
	found := strings.Index(text[cursor:], t.text)
	if found == -1 {
		return -1
	}
	found += cursor
	if found > limit {
		return -1
	}
	return found
}

// graphemeBoundaries returns the byte offset of every grapheme-cluster boundary,
// including 0 and the end. Compromise splits on codepoints, so it can report a
// word starting in the middle of a ZWJ emoji sequence; a word that begins or ends
// mid-cluster would leave a fragment behind when its neighbour is dropped.
func graphemeBoundaries(text string) map[int]struct{} {
	boundaries := map[int]struct{}{0: {}, len(text): {}}
	offset := 0
	rest := text
	for len(rest) > 0 {
		cluster, remainder, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		offset += len(cluster)
		boundaries[offset] = struct{}{}
		rest = remainder
	}
	return boundaries
}

// classifyRegion classifies one span of prose. Terms whose offset cannot be
// recovered exactly are dropped from the result, which leaves their text in the
// assembled output untouched — a lost classification costs savings, never meaning.
func classifyRegion(text string, region Region, boundaries map[int]struct{}) []ClassifiedWord {
	source := text[region.Start:region.End]
	words := []ClassifiedWord{}
	cursor := 0
	for _, t := range parseTerms(source) {
		if t.text == "" {
			cursor += len(t.pre) + len(t.post)
			continue
		}
		found := locate(source, t, cursor)
		if found == -1 {
			cursor += len(t.pre) + len(t.text) + len(t.post)
			continue
		}
		words = append(words, ClassifiedWord{
			Text:      t.text,
			Start:     region.Start + found,
			End:       region.Start + found + len(t.text),
			WordClass: classOf(t.tags),
		})
		cursor = found + len(t.text) + len(t.post)
	}
	return onGraphemeBoundaries(words, boundaries)
}

// onGraphemeBoundaries keeps only words whose both ends fall on a grapheme
// boundary. Widening one instead would change its text and break the slice
// invariant, so a word that straddles a cluster is discarded and its characters
// stay in the gap, where they are copied through verbatim.
func onGraphemeBoundaries(words []ClassifiedWord, boundaries map[int]struct{}) []ClassifiedWord {
	kept := make([]ClassifiedWord, 0, len(words))
	for _, w := range words {
		_, startOK := boundaries[w.Start]
		_, endOK := boundaries[w.End]
		if startOK && endOK {
			kept = append(kept, w)
		}
	}
	return kept
}

// ClassifyWords classifies every word inside the prose regions, in ascending
// offset order. Protected regions contribute nothing, so no word inside one can
// ever be selected for removal.
//
// Each region is parsed on its own so compromise sees whole sentences and can
// disambiguate by context — `book` is a verb in "book a flight" and a noun in "the
// book is here" — while offsets stay relative to a slice whose position in the
// original string is known.
//
// The boundary set is computed once for the whole text rather than per region;
// the TS recomputes it inside the loop, which is O(regions x len) for a result
// that does not vary by region.
//
// Every returned word satisfies text[word.Start:word.End] == word.Text.
func ClassifyWords(text string, regions []Region) []ClassifiedWord {
	boundaries := graphemeBoundaries(text)
	words := []ClassifiedWord{}
	for _, region := range regions {
		if region.Kind != "prose" {
			continue
		}
		words = append(words, classifyRegion(text, region, boundaries)...)
	}
	return words
}
