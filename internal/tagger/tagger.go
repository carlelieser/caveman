package tagger

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// pictographic stands in for compromise's
// /^[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u, the one pattern RE2 cannot
// express. The rule tests only the first rune, as the anchored JS regex does.
//
// The table is the UNION of both properties, not Extended_Pictographic alone: the
// 31 regional indicators (U+1F1E6-1F1FF) that build flag sequences carry
// Emoji_Presentation without being pictographic, and dropping them would leave a
// flag untagged.
func pictographic(s string) bool {
	for _, r := range s {
		return unicode.Is(pictographicTable, r)
	}
	return false
}

// IsPictographic reports whether r is in the same table pictographic tests
// against, for callers that hold a rune rather than the head of a string.
func IsPictographic(r rune) bool {
	return unicode.Is(pictographicTable, r)
}

var compileOnce sync.Once

// compileAll builds every generated pattern once. A pattern that fails here is a
// generator bug, so it panics rather than silently skipping the rule.
func compileAll() {
	compileOnce.Do(func() {
		lists := [][]regexRule{regexText, regexNormal, regexNumbers}
		for _, list := range lists {
			for i := range list {
				if list[i].pattern != "" {
					list[i].re = regexp.MustCompile(list[i].pattern)
				}
			}
		}
		for k := range endsWith {
			for i := range endsWith[k] {
				if endsWith[k][i].pattern != "" {
					endsWith[k][i].re = regexp.MustCompile(endsWith[k][i].pattern)
				}
			}
		}
		var compileRegs func(regs []reg)
		compileRegs = func(regs []reg) {
			for i := range regs {
				if regs[i].pattern != "" {
					regs[i].re = regexp.MustCompile(regs[i].pattern)
				}
				for j := range regs[i].choices {
					compileRegs(regs[i].choices[j])
				}
			}
		}
		for i := range matchRules {
			compileRegs(matchRules[i].regs)
			compileRegs(matchRules[i].notIf)
		}
	})
}

func (r *regexRule) matches(s string) bool {
	if r.predicate == "pictographic" {
		return pictographic(s)
	}
	if r.re == nil {
		return false
	}
	return r.re.MatchString(s)
}

// ---- contractions ----

type contraction struct {
	word   string
	before string
	after  string
	out    []string
}

func splitApostrophe(normal string) (before, after string, ok bool) {
	if !strings.Contains(normal, "'") {
		return "", "", false
	}
	parts := strings.SplitN(normal, "'", 2)
	return parts[0], parts[1], true
}

var (
	alwaysDid = map[string]bool{"what": true, "how": true, "when": true, "where": true, "why": true}
	useWould  = map[string]bool{"be": true, "go": true, "start": true, "think": true, "need": true}
	useHad    = map[string]bool{"been": true, "gone": true}
)

func apostropheD(terms []*Term, i int) []string {
	before := strings.SplitN(terms[i].Normal, "'", 2)[0]
	if alwaysDid[before] {
		return []string{before, "did"}
	}
	if i+1 < len(terms) {
		if useHad[terms[i+1].Normal] {
			return []string{before, "had"}
		}
		if useWould[terms[i+1].Normal] {
			return []string{before, "would"}
		}
	}
	return nil
}

var reNT = regexp.MustCompile(`n't`)

func apostropheT(terms []*Term, i int) []string {
	if terms[i].Normal == "ain't" || terms[i].Normal == "aint" {
		return nil
	}
	return []string{reNT.ReplaceAllString(terms[i].Normal, ""), "not"}
}

var (
	reFrenchFem  = regexp.MustCompile(`(e|é|aison|sion|tion)$`)
	reFrenchMasc = regexp.MustCompile(`(age|isme|acle|ege|oire)$`)
)

func frenchL(terms []*Term, i int) []string {
	after := afterPart(terms[i].Normal)
	if strings.HasSuffix(after, "e") {
		return []string{"la", after}
	}
	return []string{"le", after}
}

func frenchD(terms []*Term, i int) []string {
	after := afterPart(terms[i].Normal)
	if after != "" && reFrenchFem.MatchString(after) && !reFrenchMasc.MatchString(after) {
		return []string{"du", after}
	}
	if strings.HasSuffix(after, "s") {
		return []string{"des", after}
	}
	return []string{"de", after}
}

func frenchJ(terms []*Term, i int) []string {
	return []string{"je", afterPart(terms[i].Normal)}
}

func afterPart(normal string) string {
	parts := strings.SplitN(normal, "'", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func thereHas(terms []*Term, i int) []string {
	for k := i + 1; k < 5 && k < len(terms); k++ {
		if terms[k].Normal == "been" {
			return []string{"there", "has"}
		}
	}
	return []string{"there", "is"}
}

func knownContraction(term *Term, before, after string) []string {
	for _, o := range contractions {
		if o.word != "" && o.word == term.Normal {
			return o.out
		}
		if after != "" && o.after != "" && after == o.after {
			return append([]string{before}, o.out...)
		}
		if before != "" && o.before != "" && before == o.before && len(after) > 2 {
			return append(append([]string{}, o.out...), after)
		}
	}
	return nil
}

var (
	reNumDash    = regexp.MustCompile(`^[0-9][^-–—]*[-–—].*?[0-9]`)
	reIsRange    = regexp.MustCompile(`(?i)^([0-9.]{1,4}[a-z]{0,2}) ?[-–—] ?([0-9]{1,4}[a-z]{0,2})$`)
	reTimeRange  = regexp.MustCompile(`(?i)^([0-9]{1,2}(:[0-9][0-9])?(am|pm)?) ?[-–—] ?([0-9]{1,2}(:[0-9][0-9])?(am|pm)?)$`)
	rePhoneNum   = regexp.MustCompile(`^[0-9]{3}-[0-9]{4}$`)
	reNumberUnit = regexp.MustCompile(`^([+-]?[0-9][.,0-9]*)([a-z°²³µ/]+)$`)
)

func numberRange(term *Term) []string {
	if parts := reIsRange.FindStringSubmatch(term.Text); parts != nil {
		if term.Tags.has("PhoneNumber") || rePhoneNum.MatchString(term.Text) {
			return nil
		}
		return []string{parts[1], "to", parts[2]}
	}
	if parts := reTimeRange.FindStringSubmatch(term.Text); parts != nil {
		return []string{parts[1], "to", parts[4]}
	}
	return nil
}

func numberUnit(term *Term) []string {
	parts := reNumberUnit.FindStringSubmatch(term.Text)
	if parts == nil {
		return nil
	}
	unit := strings.ToLower(strings.TrimSpace(parts[2]))
	if _, ok := numberSuffixes[unit]; ok {
		return nil
	}
	return []string{parts[1], unit}
}

// spliceContraction replaces one term with the words it expands to. Only the
// first keeps the source text, which is what leaves the rest offset-less.
func spliceContraction(terms []*Term, w int, words []string) []*Term {
	if len(words) == 0 {
		return terms
	}
	// compromise builds the expansion with fromText, which runs normal + lexicon +
	// preTagger over the new words before splicing them in. Skipping that is why an
	// expanded unit like the 'ms' of '317ms' would otherwise reach classify with no
	// Abbreviation tag, and so resolve to number instead of other.
	made := taggedExpansion(words)
	made[0].Pre = terms[w].Pre
	made[len(made)-1].Post = terms[w].Post
	made[0].Text = terms[w].Text
	made[0].Normal = terms[w].Normal

	out := make([]*Term, 0, len(terms)+len(made)-1)
	out = append(out, terms[:w]...)
	out = append(out, made...)
	out = append(out, terms[w+1:]...)
	return out
}

// taggedExpansion tokenizes and tags the words a contraction expands to, then
// turns them into implicit terms carrying no text of their own.
func taggedExpansion(words []string) []*Term {
	sub := tokenize(strings.Join(words, " "))
	flat := []*Term{}
	for _, sentence := range sub {
		flat = append(flat, sentence...)
	}
	// a word that retokenized into a different count cannot be lined up, so fall
	// back to bare terms rather than mismatching text to tags
	if len(flat) != len(words) {
		flat = make([]*Term, len(words))
		for i, word := range words {
			flat[i] = newTerm()
			flat[i].Normal = word
			flat[i].Text = word
		}
	}
	lexiconPass([][]*Term{flat})
	preTagger([][]*Term{flat})
	for i, word := range flat {
		word.Implicit = word.Text
		word.Machine = word.Text
		word.Pre = ""
		word.Post = ""
		word.Text = ""
		word.Normal = ""
		_ = i
	}
	return flat
}

func expandContractions(doc [][]*Term) [][]*Term {
	for n := range doc {
		terms := doc[n]
		for i := len(terms) - 1; i >= 0; i-- {
			before, after, hasApos := splitApostrophe(terms[i].Normal)
			if !hasApos {
				before, after = "", ""
			}
			words := knownContraction(terms[i], before, after)
			if words == nil && hasApos {
				switch after {
				case "t":
					words = apostropheT(terms, i)
				case "d":
					words = apostropheD(terms, i)
				}
			}
			if words == nil && hasApos {
				switch before {
				case "j":
					words = frenchJ(terms, i)
				case "l":
					words = frenchL(terms, i)
				case "d":
					words = frenchD(terms, i)
				}
			}
			if before == "there" && after == "s" {
				words = thereHas(terms, i)
			}
			if words != nil {
				terms = spliceContraction(terms, i, words)
				continue
			}
			if reNumDash.MatchString(terms[i].Normal) {
				if words := numberRange(terms[i]); words != nil {
					at := i
					terms = spliceContraction(terms, i, words)
					setTag(terms[at:at+len(words)], "NumberRange", false)
					if at+2 < len(terms) && terms[at+2].Tags.has("Time") {
						setTag([]*Term{terms[at]}, "Time", false)
					}
				}
				continue
			}
			if words := numberUnit(terms[i]); words != nil {
				at := i
				terms = spliceContraction(terms, i, words)
				setTag([]*Term{terms[at+1]}, "Unit", false)
			}
		}
		doc[n] = terms
	}
	return doc
}

// ---- lexicon pass ----

var reLexPrefix = regexp.MustCompile(`^(under|over|mis|re|un|dis|semi|pre|post)-?`)

var allowPrefix = map[string]bool{
	"Verb": true, "Infinitive": true, "PastTense": true, "Gerund": true,
	"PresentTense": true, "Adjective": true, "Participle": true,
}

func lexiconTags(word string) ([]string, bool) {
	if tags, ok := lexiconMulti[word]; ok {
		return tags, true
	}
	if tag, ok := lexicon[word]; ok {
		return []string{tag}, true
	}
	return nil, false
}

func multiWordLookup(terms []*Term, start int) bool {
	word := terms[start].lookupWord()
	n, ok := multiCache[word]
	if !ok || start+1 >= len(terms) {
		return false
	}
	end := start + n - 1
	for i := end; i > start; i-- {
		if i >= len(terms) {
			continue
		}
		words := terms[start : i+1]
		if len(words) <= 1 {
			return false
		}
		parts := make([]string, len(words))
		for j, t := range words {
			parts[j] = t.lookupWord()
		}
		str := strings.Join(parts, " ")
		if tags, found := lexiconTags(str); found {
			for _, tag := range tags {
				setTag(words, tag, false)
			}
			if len(tags) == 2 && (tags[0] == "PhrasalVerb" || tags[1] == "PhrasalVerb") {
				setTag([]*Term{words[1]}, "Particle", false)
			}
			return true
		}
	}
	return false
}

func singleWordLookup(t *Term) bool {
	word := t.lookupWord()
	if tags, ok := lexiconTags(word); ok {
		for _, tag := range tags {
			setTag([]*Term{t}, tag, false)
		}
		return true
	}
	for _, alias := range t.Alias {
		if tags, ok := lexiconTags(alias); ok {
			for _, tag := range tags {
				setTag([]*Term{t}, tag, false)
			}
			return true
		}
	}
	if reLexPrefix.MatchString(word) {
		stem := reLexPrefix.ReplaceAllString(word, "")
		if tag, ok := lexicon[stem]; ok && len(stem) > 3 && allowPrefix[tag] {
			setTag([]*Term{t}, tag, false)
			return true
		}
	}
	return false
}

func lexiconPass(doc [][]*Term) {
	for _, terms := range doc {
		for i := range terms {
			if terms[i].Tags.size() == 0 {
				if !multiWordLookup(terms, i) {
					singleWordLookup(terms[i])
				}
			}
		}
	}
}

// ---- quickSplit ----

var reSplitHere = regexp.MustCompile(`[,:;]`)
var reIsNum = regexp.MustCompile(`^[0-9]+$`)

var maybeDate = map[string]bool{"may": true, "april": true, "august": true, "jan": true}

func splitOn(terms []*Term, i int) bool {
	if i >= len(terms) {
		return false
	}
	term := terms[i]
	if term.Normal == "like" || maybeDate[term.Normal] {
		return false
	}
	if term.Tags.has("Place") || term.Tags.has("Date") {
		return false
	}
	if i-1 >= 0 {
		last := terms[i-1]
		if last.Tags.has("Date") || maybeDate[last.Normal] {
			return false
		}
		if last.Tags.has("Adjective") || term.Tags.has("Adjective") {
			return false
		}
	}
	str := term.Normal
	if n := len(str); n == 1 || n == 2 || n == 4 {
		if reIsNum.MatchString(str) {
			return false
		}
	}
	return true
}

func quickSplit(doc [][]*Term) [][]*Term {
	arr := [][]*Term{}
	for _, terms := range doc {
		start := 0
		for i, term := range terms {
			if reSplitHere.MatchString(term.Post) && splitOn(terms, i+1) {
				arr = append(arr, terms[start:i+1])
				start = i + 1
			}
		}
		if start < len(terms) {
			arr = append(arr, terms[start:])
		}
	}
	return arr
}

// ---- preTagger passes ----

var reHasColon = regexp.MustCompile(`:`)

func byPunctuation(terms []*Term) {
	if len(terms) >= 3 && reHasColon.MatchString(terms[0].Post) {
		next := terms[1]
		if next.Tags.has("Value") || next.Tags.has("Email") || next.Tags.has("PhoneNumber") {
			return
		}
		setTag([]*Term{terms[0]}, "Expression", false)
	}
}

func byHyphen(terms []*Term, i int) {
	if terms[i].Post == "-" && i+1 < len(terms) {
		setTag([]*Term{terms[i], terms[i+1]}, "Hyphenated", false)
	}
}

var reSwitchPrefix = regexp.MustCompile(`^(under|over|mis|re|un|dis|semi)-?`)

func tagSwitch(t *Term) {
	if form, ok := switches[t.Normal]; ok {
		t.Switch = form
		return
	}
	if reSwitchPrefix.MatchString(t.Normal) {
		stem := reSwitchPrefix.ReplaceAllString(t.Normal, "")
		if len(stem) > 3 {
			if form, ok := switches[stem]; ok {
				t.Switch = form
			}
		}
	}
}

var (
	reTitleCase  = regexp.MustCompile(`^\p{Lu}[\p{Ll}'’]`)
	reHasNumber  = regexp.MustCompile(`[0-9]`)
	reRomanNum   = regexp.MustCompile(`^[IVXLCDM]{2,}$`)
	reHasIVX     = regexp.MustCompile(`[IVX]`)
	reRomanValid = regexp.MustCompile(`^M{0,4}(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)
	reQuoteEnd   = regexp.MustCompile(`["']$`)
)

var notProper = []string{"Date", "Month", "WeekDay", "Unit", "Expression"}
var romanNope = map[string]bool{"li": true, "dc": true, "md": true, "dm": true, "ml": true}

func checkCase(terms []*Term, i int) bool {
	term := terms[i]
	index := term.Index[1]
	str := term.Text
	if index != 0 && reTitleCase.MatchString(str) && !reHasNumber.MatchString(str) {
		for _, tag := range notProper {
			if term.Tags.has(tag) {
				return false
			}
		}
		if reQuoteEnd.MatchString(term.Pre) {
			return false
		}
		if term.Normal == "the" {
			return false
		}
		fillTags(terms, i)
		if !term.Tags.has("Noun") && !term.Frozen {
			term.Tags.clear()
		}
		fastTag(term, "ProperNoun")
		return true
	}
	if len([]rune(str)) >= 2 && reRomanNum.MatchString(str) && reHasIVX.MatchString(str) &&
		reRomanValid.MatchString(str) && !romanNope[term.Normal] {
		fastTag(term, "RomanNumeral")
		return true
	}
	return false
}

func suffixLoop(str string, patterns []map[string]string) string {
	runes := []rune(str)
	length := len(runes)
	max := 7
	if length <= max {
		max = length - 1
	}
	for i := max; i > 1; i-- {
		suffix := string(runes[length-i:])
		if i < len(patterns) && patterns[i] != nil {
			if tag, ok := patterns[i][suffix]; ok {
				return tag
			}
		}
	}
	return ""
}

func tagBySuffix(t *Term) bool {
	if t.Tags.size() != 0 {
		return false
	}
	if tag := suffixLoop(t.Normal, suffixPatterns); tag != "" {
		fastTag(t, tag)
		t.Conf = 0.7
		return true
	}
	if t.Implicit != "" {
		if tag := suffixLoop(t.Implicit, suffixPatterns); tag != "" {
			fastTag(t, tag)
			t.Conf = 0.7
			return true
		}
	}
	return false
}

var reMatchApostrophe = regexp.MustCompile(`['‘’‛‵′` + "`" + `´]`)

func doRegs(str string, regs []regexRule) *regexRule {
	for i := range regs {
		if regs[i].matches(str) {
			return &regs[i]
		}
	}
	return nil
}

func doEndsWith(str string) *regexRule {
	runes := []rune(str)
	if len(runes) == 0 {
		return nil
	}
	char := string(runes[len(runes)-1])
	regs, ok := endsWith[char]
	if !ok {
		return nil
	}
	for i := range regs {
		if regs[i].matches(str) {
			return &regs[i]
		}
	}
	return nil
}

func checkRegex(t *Term) bool {
	normal := t.lookupWord()
	text := t.Text
	if reMatchApostrophe.MatchString(t.Post) && !reMatchApostrophe.MatchString(t.Pre) {
		text += strings.TrimSpace(t.Post)
	}
	rule := doRegs(text, regexText)
	if rule == nil {
		rule = doRegs(normal, regexNormal)
	}
	if rule == nil && reHasNumber.MatchString(normal) {
		rule = doRegs(normal, regexNumbers)
	}
	if rule == nil && t.Tags.size() == 0 {
		rule = doEndsWith(normal)
	}
	if rule != nil {
		for _, tag := range rule.tags {
			setTag([]*Term{t}, tag, false)
		}
		t.Conf = 0.6
		return true
	}
	return false
}

func prefixLoop(str string, patterns []map[string]string) string {
	runes := []rune(str)
	length := len(runes)
	max := 7
	if max > length-3 {
		max = length - 3
	}
	for i := max; i > 2; i-- {
		prefix := string(runes[:i])
		if i < len(patterns) && patterns[i] != nil {
			if tag, ok := patterns[i][prefix]; ok {
				return tag
			}
		}
	}
	return ""
}

func checkPrefix(t *Term) bool {
	if t.Tags.size() != 0 {
		return false
	}
	if tag := prefixLoop(t.Normal, prefixPatterns); tag != "" {
		fastTag(t, tag)
		t.Conf = 0.5
		return true
	}
	return false
}

var dateWords = map[string]bool{
	"in": true, "on": true, "by": true, "until": true, "for": true, "to": true,
	"during": true, "throughout": true, "through": true, "within": true,
	"before": true, "after": true, "of": true, "this": true, "next": true,
	"last": true, "circa": true, "around": true, "post": true, "pre": true,
	"budget": true, "classic": true, "plan": true, "may": true,
}

func termAt(terms []*Term, i int) *Term {
	if i < 0 || i >= len(terms) {
		return nil
	}
	return terms[i]
}

func seemsGood(t *Term) bool {
	if t == nil {
		return false
	}
	str := t.Normal
	if str == "" {
		str = t.Implicit
	}
	if dateWords[str] {
		return true
	}
	if t.Tags.has("Date") || t.Tags.has("Month") || t.Tags.has("WeekDay") || t.Tags.has("Year") {
		return true
	}
	return t.Tags.has("ProperNoun")
}

func seemsOkay(t *Term) bool {
	if t == nil {
		return false
	}
	if t.Tags.has("Ordinal") {
		return true
	}
	if t.Tags.has("Cardinal") && len(t.Normal) < 3 {
		return true
	}
	return t.Normal == "is" || t.Normal == "was"
}

func seemsFine(t *Term) bool {
	return t != nil && (t.Tags.has("Date") || t.Tags.has("Month") || t.Tags.has("WeekDay") || t.Tags.has("Year"))
}

func tagYear(terms []*Term, i int) bool {
	term := terms[i]
	if !term.Tags.has("NumericValue") || !term.Tags.has("Cardinal") || len(term.Normal) != 4 {
		return false
	}
	num, err := strconv.Atoi(term.Normal)
	if err != nil || num == 0 {
		return false
	}
	if num <= 1400 || num >= 2100 {
		return false
	}
	last, next := termAt(terms, i-1), termAt(terms, i+1)
	if seemsGood(last) || seemsGood(next) {
		fastTag(term, "Year")
		return true
	}
	if num >= 1920 && num < 2025 {
		if seemsOkay(last) || seemsOkay(next) {
			fastTag(term, "Year")
			return true
		}
		if seemsFine(termAt(terms, i-2)) || seemsFine(termAt(terms, i+2)) {
			fastTag(term, "Year")
			return true
		}
		if last != nil && (last.Tags.has("Determiner") || last.Tags.has("Possessive")) {
			if next != nil && next.Tags.has("Noun") && !next.Tags.has("Plural") {
				fastTag(term, "Year")
				return true
			}
		}
	}
	return false
}

// ---- 3rd pass ----

var (
	reOneLetterAcr = regexp.MustCompile(`^[A-Z]('s|,)?$`)
	reIsUpperCase  = regexp.MustCompile(`^[A-Z-]+$`)
	reUpperThenS   = regexp.MustCompile(`^[A-Z]+s$`)
)

var acronymPlaces = map[string]bool{
	"la": true, "ny": true, "us": true, "dc": true, "gb": true, "uk": true,
}
var oneLetterWord = map[string]bool{"I": true, "A": true}

func isNoPeriodAcronym(t *Term) bool {
	str := t.Text
	if !reIsUpperCase.MatchString(str) {
		if len(str) > 3 && reUpperThenS.MatchString(str) {
			str = strings.TrimSuffix(str, "s")
		} else {
			return false
		}
	} else if acronymPlaces[t.Normal] {
		return true
	}
	if len(str) > 5 {
		return false
	}
	if oneLetterWord[str] {
		return false
	}
	if _, ok := lexicon[t.Normal]; ok {
		return false
	}
	if _, ok := lexiconMulti[t.Normal]; ok {
		return false
	}
	return isAcronymStr(str)
}

func checkAcronym(terms []*Term, i int) bool {
	term := terms[i]
	if term.Tags.has("RomanNumeral") || term.Tags.has("Acronym") || term.Frozen {
		return false
	}
	if isNoPeriodAcronym(term) {
		term.Tags.clear()
		fastTag(term, "Acronym", "Noun")
		if acronymPlaces[term.Normal] {
			fastTag(term, "Place")
		}
		if reUpperThenS.MatchString(term.Text) {
			fastTag(term, "Plural")
		}
		return true
	}
	if !oneLetterWord[term.Text] && reOneLetterAcr.MatchString(term.Text) {
		term.Tags.clear()
		fastTag(term, "Acronym", "Noun")
		return true
	}
	if term.Tags.has("Organization") && len(term.Text) <= 3 {
		fastTag(term, "Acronym")
		return true
	}
	if term.Tags.has("Organization") && reIsUpperCase.MatchString(term.Text) && len(term.Text) <= 6 {
		fastTag(term, "Acronym")
		return true
	}
	return false
}

var fillUncountable = []string{
	"Acronym", "Abbreviation", "ProperNoun", "Uncountable", "Possessive",
	"Pronoun", "Activity", "Honorific", "Month",
}

func setPluralSingular(t *Term) {
	if !t.Tags.has("Noun") || t.Tags.has("Plural") || t.Tags.has("Singular") {
		return
	}
	for _, tag := range fillUncountable {
		if t.Tags.has(tag) {
			return
		}
	}
	if looksPlural(t.Normal) {
		fastTag(t, "Plural")
	} else {
		fastTag(t, "Singular")
	}
}

func setTense(t *Term) {
	if t.Tags.has("Verb") && t.Tags.size() == 1 {
		if guess := getTense(t.Normal); guess != "" {
			fastTag(t, guess)
		}
	}
}

func fillTags(terms []*Term, i int) {
	term := terms[i]
	for _, tag := range term.Tags.list() {
		if info, ok := tagSet[tag]; ok {
			fastTag(term, info.parents...)
		}
	}
	setPluralSingular(term)
	setTense(term)
}

func lookAtWord(t *Term, words [][2]string) string {
	if t == nil {
		return ""
	}
	for _, pair := range words {
		if t.Normal == pair[0] {
			return pair[1]
		}
	}
	return ""
}

func lookAtTag(t *Term, tags [][2]string) string {
	if t == nil {
		return ""
	}
	for _, pair := range tags {
		if t.Tags.has(pair[0]) {
			return pair[1]
		}
	}
	return ""
}

func neighbours(terms []*Term, i int) bool {
	term := terms[i]
	if term.Tags.size() != 0 {
		return false
	}
	tag := lookAtWord(termAt(terms, i-1), neighbourLeftWords)
	if tag == "" {
		tag = lookAtWord(termAt(terms, i+1), neighbourRightWords)
	}
	if tag == "" {
		tag = lookAtTag(termAt(terms, i-1), neighbourLeftTags)
	}
	if tag == "" {
		tag = lookAtTag(termAt(terms, i+1), neighbourRightTags)
	}
	if tag != "" {
		fastTag(term, tag)
		fillTags(terms, i)
		term.Conf = 0.2
		return true
	}
	return false
}

func nounFallback(terms []*Term, i int) {
	tags := terms[i].Tags
	isEmpty := tags.size() == 0
	if tags.size() == 1 {
		if tags.has("Hyphenated") || tags.has("HashTag") || tags.has("Prefix") || tags.has("SlashedTerm") {
			isEmpty = true
		}
	}
	if isEmpty {
		fastTag(terms[i], "Noun")
		fillTags(terms, i)
		terms[i].Conf = 0.1
	}
}

var reIsTitleCase3 = regexp.MustCompile(`^\p{Lu}[\p{Ll}'’]`)

func isOrg(t *Term, i int, yelling bool) bool {
	if t == nil {
		return false
	}
	if t.Tags.has("FirstName") || t.Tags.has("Place") {
		return false
	}
	if t.Tags.has("ProperNoun") || t.Tags.has("Organization") || t.Tags.has("Acronym") {
		return true
	}
	if !yelling && reIsTitleCase3.MatchString(t.Text) {
		if i == 0 {
			return t.Tags.has("Singular")
		}
		return true
	}
	return false
}

func tagOrgs(terms []*Term, i int, yelling bool) {
	str := terms[i].lookupWord()
	if _, ok := orgWords[str]; !ok {
		return
	}
	if !isOrg(termAt(terms, i-1), i-1, yelling) {
		return
	}
	setTag([]*Term{terms[i]}, "Organization", false)
	for t := i; t >= 0; t-- {
		if isOrg(terms[t], t, yelling) {
			setTag([]*Term{terms[t]}, "Organization", false)
		} else {
			break
		}
	}
}

var rePossessive = regexp.MustCompile(`'s$`)

var placeCont = map[string]bool{
	"athletic": true, "city": true, "community": true, "eastern": true,
	"federal": true, "financial": true, "great": true, "historic": true,
	"historical": true, "local": true, "memorial": true, "municipal": true,
	"national": true, "northern": true, "provincial": true, "southern": true,
	"state": true, "western": true, "spring": true, "pine": true, "sunset": true,
	"view": true, "oak": true, "maple": true, "spruce": true, "cedar": true, "willow": true,
}

var noBefore = map[string]bool{
	"center": true, "centre": true, "way": true, "range": true,
	"bar": true, "bridge": true, "field": true, "pit": true,
}

func isPlace(t *Term, i int, yelling bool) bool {
	if t == nil {
		return false
	}
	if t.Tags.has("Organization") || t.Tags.has("Possessive") || rePossessive.MatchString(t.Normal) {
		return false
	}
	if t.Tags.has("ProperNoun") || t.Tags.has("Place") {
		return true
	}
	if !yelling && reIsTitleCase3.MatchString(t.Text) {
		if i == 0 {
			return t.Tags.has("Singular")
		}
		return true
	}
	return false
}

func tagPlaces(terms []*Term, i int, yelling bool) {
	str := terms[i].lookupWord()
	if _, ok := placeWords[str]; !ok {
		return
	}
	for n := i - 1; n >= 0; n-- {
		if placeCont[terms[n].Normal] {
			continue
		}
		if isPlace(terms[n], n, yelling) {
			setTag(terms[n:i+1], "Place", false)
			continue
		}
		break
	}
	if noBefore[str] {
		return
	}
	for n := i + 1; n < len(terms); n++ {
		if isPlace(terms[n], n, yelling) {
			setTag(terms[i:n+1], "Place", false)
			return
		}
		if terms[n].Normal == "of" || placeCont[terms[n].Normal] {
			continue
		}
		break
	}
}

var reAdhocTitle = regexp.MustCompile(`^[A-Z][a-z]`)

func isCapital(terms []*Term, i int) string {
	if terms[i].Tags.has("ProperNoun") && reAdhocTitle.MatchString(terms[i].Text) {
		return "Noun"
	}
	return ""
}

func isAlone(terms []*Term, i int, tag string) string {
	if i == 0 && len(terms) < 2 {
		return tag
	}
	return ""
}

func isEndNoun(terms []*Term, i int) string {
	if i+1 >= len(terms) && i-1 >= 0 && terms[i-1].Tags.has("Determiner") {
		return "Noun"
	}
	return ""
}

func isStart(terms []*Term, i int, tag string) string {
	if i == 0 && len(terms) > 3 {
		return tag
	}
	return ""
}

func adhocSwitch(form string, terms []*Term, i int) string {
	switch form {
	case "Adj|Gerund", "Actor|Verb", "Adj|Past", "Adj|Present", "Noun|Gerund", "Person|Noun":
		return isCapital(terms, i)
	case "Adj|Noun":
		if tag := isCapital(terms, i); tag != "" {
			return tag
		}
		return isEndNoun(terms, i)
	case "Noun|Verb":
		if i > 0 {
			if tag := isCapital(terms, i); tag != "" {
				return tag
			}
		}
		return isAlone(terms, i, "Infinitive")
	case "Plural|Verb":
		if tag := isCapital(terms, i); tag != "" {
			return tag
		}
		if tag := isAlone(terms, i, "PresentTense"); tag != "" {
			return tag
		}
		return isStart(terms, i, "Plural")
	case "Person|Verb":
		if i != 0 {
			return isCapital(terms, i)
		}
		return ""
	case "Person|Adj":
		if i == 0 && len(terms) > 1 {
			return "Person"
		}
		if isCapital(terms, i) != "" {
			return "Person"
		}
		return ""
	}
	return ""
}

func checkWordClue(t *Term, obj map[string]string) string {
	if t == nil || obj == nil {
		return ""
	}
	str := t.Normal
	if str == "" {
		str = t.Implicit
	}
	return obj[str]
}

// checkTagClue sorts the term's tags so the most specific one wins, which is
// what compromise's parents-length sort does.
func checkTagClue(t *Term, obj map[string]string) string {
	if t == nil || obj == nil {
		return ""
	}
	tags := t.Tags.list()
	sortByParents(tags)
	for _, tag := range tags {
		if v, ok := obj[tag]; ok && v != "" {
			return v
		}
	}
	return ""
}

func sortByParents(tags []string) {
	// JS Array.sort with this comparator is not a total order; a stable insertion
	// sort reproduces V8's behaviour for the small arrays involved here.
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0; j-- {
			a, b := tags[j-1], tags[j]
			numA, numB := 0, 0
			if info, ok := tagSet[a]; ok {
				numA = len(info.parents)
			}
			if info, ok := tagSet[b]; ok {
				numB = len(info.parents)
			}
			if numA > numB {
				break
			}
			if numA == numB {
				break
			}
			tags[j-1], tags[j] = tags[j], tags[j-1]
		}
	}
}

func pickTag(terms []*Term, i int, c clueSet) string {
	beforeIndex := i - 1
	if i-1 >= 0 && terms[i-1].Text == "also" {
		beforeIndex = i - 2
		if beforeIndex < 0 {
			beforeIndex = 0
		}
	}
	tag := checkWordClue(termAt(terms, i+1), c.afterWords)
	if tag == "" {
		tag = checkWordClue(termAt(terms, beforeIndex), c.beforeWords)
	}
	if tag == "" {
		tag = checkTagClue(termAt(terms, beforeIndex), c.beforeTags)
	}
	if tag == "" {
		tag = checkTagClue(termAt(terms, i+1), c.afterTags)
	}
	return tag
}

func doSwitches(terms []*Term, i int) {
	term := terms[i]
	if term.Switch == "" {
		return
	}
	if term.Tags.has("Acronym") || term.Tags.has("PhrasalVerb") {
		return
	}
	form := term.Switch
	tag := ""
	if c, ok := clues[form]; ok {
		tag = pickTag(terms, i, c)
	}
	if adhoc := adhocSwitch(form, terms, i); adhoc != "" {
		tag = adhoc
	}
	if tag != "" {
		setTag([]*Term{term}, tag, false)
		fillTags(terms, i)
	}
}

var verbTypes = []string{"PastTense", "PresentTense", "Auxiliary", "Modal", "Particle"}

func verbType(t *Term) {
	if !t.Tags.has("Verb") {
		return
	}
	for _, typ := range verbTypes {
		if t.Tags.has(typ) {
			return
		}
	}
	setTag([]*Term{t}, "Infinitive", false)
}

var imperativeBeside = map[string]bool{
	"there": true, "this": true, "it": true, "him": true, "her": true, "us": true,
}

func imperative(terms []*Term) {
	if len(terms) == 0 {
		return
	}
	t := terms[0]
	isRight := t.Switch == "Noun|Verb" || t.Tags.has("Infinitive")
	if !isRight || len(terms) < 2 {
		return
	}
	if len(terms) < 4 && !imperativeBeside[terms[1].Normal] {
		return
	}
	if !t.Tags.has("PhrasalVerb") {
		if _, ok := multiCache[t.Normal]; ok {
			return
		}
	}
	if terms[1].Tags.has("Noun") || terms[1].Tags.has("Determiner") {
		soonVerb := false
		for _, term := range terms[1:min(3, len(terms))] {
			if term.Tags.has("Verb") {
				soonVerb = true
				break
			}
		}
		if !soonVerb {
			setTag([]*Term{t}, "Imperative", false)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var reLowerStart = regexp.MustCompile(`^[a-z]`)

func ignoreCase(terms []*Term) bool {
	notProperCount := 0
	for _, t := range terms {
		if !t.Tags.has("ProperNoun") {
			notProperCount++
		}
	}
	if notProperCount <= 3 {
		return false
	}
	for _, t := range terms {
		if reLowerStart.MatchString(t.Text) {
			return false
		}
	}
	return true
}

func secondPass(terms []*Term, isYelling bool) {
	for i := range terms {
		if terms[i].Frozen {
			continue
		}
		tagSwitch(terms[i])
		if !isYelling {
			checkCase(terms, i)
		}
		tagBySuffix(terms[i])
		checkRegex(terms[i])
		checkPrefix(terms[i])
		tagYear(terms, i)
	}
}

func thirdPass(terms []*Term, isYelling bool) {
	for i := range terms {
		found := checkAcronym(terms, i)
		fillTags(terms, i)
		if !found {
			found = neighbours(terms, i)
		}
		if !found {
			nounFallback(terms, i)
		}
	}
	for i := range terms {
		if terms[i].Frozen {
			continue
		}
		tagOrgs(terms, i, isYelling)
		tagPlaces(terms, i, isYelling)
		doSwitches(terms, i)
		verbType(terms[i])
		byHyphen(terms, i)
	}
	imperative(terms)
}

func preTagger(doc [][]*Term) [][]*Term {
	for _, terms := range doc {
		if len(terms) > 0 {
			byPunctuation(terms)
		}
	}
	clauses := quickSplit(doc)
	for _, terms := range clauses {
		isYelling := ignoreCase(terms)
		secondPass(terms, isYelling)
		thirdPass(terms, isYelling)
	}
	return clauses
}

// Parse tokenizes and tags text, returning one slice of terms per sentence.
func Parse(text string) [][]*Term {
	compileAll()
	doc := tokenize(text)
	doc = expandContractions(doc)
	reindex(doc)
	freezePass(doc)
	lexiconPass(doc)
	preTagger(doc)
	doc = contractionTwo(doc)
	postTagger(doc)
	unfreezePass(doc)
	return doc
}

func reindex(doc [][]*Term) {
	for n := range doc {
		for i := range doc[n] {
			doc[n][i].Index = [2]int{n, i}
		}
	}
}

// freezePass tags the phrases in the frozen lexicon and marks them frozen, which
// is what stops a later pass from retagging 'by the time' away from Conjunction.
// The hook order is freeze, then lexicon, then preTagger; unfreeze runs at the end.
func freezePass(doc [][]*Term) {
	for _, terms := range doc {
		for i := 0; i < len(terms); i++ {
			word := terms[i].lookupWord()
			if n, ok := multiCache[word]; ok && i+1 < len(terms) {
				end := i + n - 1
				for k := end; k > i; k-- {
					if k >= len(terms) {
						continue
					}
					words := terms[i : k+1]
					parts := make([]string, len(words))
					for j, t := range words {
						parts[j] = t.lookupWord()
					}
					if tag, found := frozenLex[strings.Join(parts, " ")]; found {
						setTag(words, tag, false)
						for _, t := range words {
							t.Frozen = true
						}
					}
				}
			}
			if tag, ok := frozenLex[word]; ok {
				setTag([]*Term{terms[i]}, tag, false)
				terms[i].Frozen = true
			}
		}
	}
}

func unfreezePass(doc [][]*Term) {
	for _, terms := range doc {
		for _, t := range terms {
			t.Frozen = false
		}
	}
}
