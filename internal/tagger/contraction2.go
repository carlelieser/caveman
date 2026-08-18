package tagger

import "strings"

// contraction-two runs after the preTagger, when the tags it needs to decide
// between "spencer's house" and "spencer's here" finally exist. contraction-one
// handled the forms that need no tags; these need the tense of what follows.

var (
	possessiveBanList = map[string]bool{
		"that": true, "there": true, "let": true, "here": true, "everywhere": true,
	}
	beforePossessive = map[string]bool{"in": true, "by": true, "for": true}
	adjLike          = map[string]bool{"too": true, "also": true, "enough": true, "about": true}
	nounLike         = map[string]bool{
		"is": true, "are": true, "did": true, "were": true, "could": true,
		"should": true, "must": true, "had": true, "have": true,
	}
)

func isPossessive(terms []*Term, i int) bool {
	term := terms[i]
	if possessiveBanList[term.lookupWord()] {
		return false
	}
	if term.Tags.has("Possessive") {
		return true
	}
	if term.Tags.has("QuestionWord") {
		return false
	}
	if term.Normal == "he's" || term.Normal == "she's" {
		return false
	}
	next := termAt(terms, i+1)
	if next == nil {
		return true
	}
	if term.Normal == "it's" {
		return next.Tags.has("Noun")
	}
	if next.Switch == "Noun|Gerund" {
		next2 := termAt(terms, i+2)
		if next2 == nil {
			return term.Tags.has("Actor") || term.Tags.has("ProperNoun")
		}
		if next2.Tags.has("Copula") {
			return true
		}
		return false
	}
	if next.Tags.has("Verb") {
		if next.Tags.has("Infinitive") {
			return true
		}
		if next.Tags.has("Gerund") {
			return false
		}
		if next.Tags.has("PresentTense") {
			return true
		}
		return false
	}
	if next.Switch == "Adj|Noun" {
		two := termAt(terms, i+2)
		if two == nil {
			return false
		}
		if nounLike[two.Normal] {
			return true
		}
		if adjLike[two.Normal] {
			return false
		}
	}
	if next.Tags.has("Noun") {
		nextStr := next.lookupWord()
		if nextStr == "here" || nextStr == "there" || nextStr == "everywhere" {
			return false
		}
		if next.Tags.has("Possessive") {
			return false
		}
		if next.Tags.has("ProperNoun") && !term.Tags.has("ProperNoun") {
			return false
		}
		return true
	}
	if prev := termAt(terms, i-1); prev != nil && beforePossessive[prev.Normal] {
		return true
	}
	if next.Tags.has("Adjective") {
		two := termAt(terms, i+2)
		if two == nil {
			return false
		}
		if two.Tags.has("Noun") && !two.Tags.has("Pronoun") {
			str := next.Normal
			if str == "above" || str == "below" || str == "behind" {
				return false
			}
			return true
		}
		if two.Switch == "Noun|Verb" {
			return true
		}
		return false
	}
	if next.Tags.has("Value") {
		return true
	}
	return false
}

var (
	sHasWords = map[string]bool{"been": true, "become": true}
	sIsWords  = map[string]bool{
		"what": true, "how": true, "when": true, "if": true, "too": true,
	}
	sAdjLike = map[string]bool{"too": true, "also": true, "enough": true}
)

// isOrHas reads the tense of the next verb to tell "the meeting's been" from
// "the cat's sleeping".
func isOrHas(terms []*Term, i int) string {
	for o := i + 1; o < len(terms); o++ {
		t := terms[o]
		if sHasWords[t.Normal] {
			return "has"
		}
		if sIsWords[t.Normal] {
			return "is"
		}
		if t.Tags.has("Gerund") || t.Tags.has("Determiner") || t.Tags.has("Adjective") {
			return "is"
		}
		if t.Switch == "Adj|Past" {
			if next := termAt(terms, o+1); next != nil {
				if sAdjLike[next.Normal] || next.Tags.has("Preposition") {
					return "is"
				}
			}
		}
		if t.Tags.has("PastTense") {
			if next := termAt(terms, o+1); next != nil && next.Normal == "for" {
				return "is"
			}
			return "has"
		}
	}
	return "is"
}

func apostropheS(terms []*Term, i int) []string {
	before := strings.SplitN(terms[i].Normal, "'", 2)[0]
	if before == "let" {
		return []string{before, "us"}
	}
	if before == "there" {
		if t := termAt(terms, i+1); t != nil && t.Tags.has("Plural") {
			return []string{before, "are"}
		}
	}
	if isOrHas(terms, i) == "has" {
		return []string{before, "has"}
	}
	return []string{before, "is"}
}

var (
	dHadWords = map[string]bool{
		"better": true, "done": true, "before": true, "it": true, "had": true,
	}
	dWouldWords = map[string]bool{"have": true, "be": true}
)

func hadOrWould(terms []*Term, i int) string {
	for o := i + 1; o < len(terms); o++ {
		t := terms[o]
		if dHadWords[t.Normal] {
			return "had"
		}
		if dWouldWords[t.Normal] {
			return "would"
		}
		if t.Tags.has("PastTense") || t.Switch == "Adj|Past" {
			return "had"
		}
		if t.Tags.has("PresentTense") || t.Tags.has("Infinitive") {
			return "would"
		}
		if t.Tags.has("Determiner") {
			return "had"
		}
		if t.Tags.has("Adjective") {
			return "would"
		}
	}
	return ""
}

func apostropheDTwo(terms []*Term, i int) []string {
	before := strings.SplitN(terms[i].Normal, "'", 2)[0]
	if before == "how" || before == "what" {
		return []string{before, "did"}
	}
	if hadOrWould(terms, i) == "had" {
		return []string{before, "had"}
	}
	return []string{before, "would"}
}

// contractionTwo expands the apostrophe forms that needed tags to disambiguate,
// re-tagging each expansion the way compromise's reTag does.
func contractionTwo(doc [][]*Term) [][]*Term {
	for n := range doc {
		terms := doc[n]
		for i := len(terms) - 1; i >= 0; i-- {
			if terms[i].Implicit != "" {
				continue
			}
			_, after, hasApos := splitApostrophe(terms[i].Normal)
			if !hasApos {
				continue
			}
			var words []string
			switch after {
			case "d":
				words = apostropheDTwo(terms, i)
			case "t":
				words = apostropheT(terms, i)
			case "s":
				if isPossessive(terms, i) {
					setTag([]*Term{terms[i]}, "Possessive", false)
					continue
				}
				words = apostropheS(terms, i)
			}
			if words == nil {
				continue
			}
			terms = spliceContraction(terms, i, words)
			retagAround(terms, i, len(words))
		}
		doc[n] = terms
	}
	return doc
}

// retagAround re-runs the lexicon and preTagger over the spliced terms and their
// immediate neighbours, which is the window compromise's reTag uses.
func retagAround(terms []*Term, start, length int) {
	end := start + length
	if start > 0 {
		start--
	}
	if end < len(terms) {
		end++
	}
	if start >= end {
		return
	}
	window := terms[start:end]
	lexiconPass([][]*Term{window})
	preTagger([][]*Term{window})
	for i := range terms {
		terms[i].Index[1] = i
	}
}
