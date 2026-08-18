package tagger

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reInitSplit  = regexp.MustCompile(`([.!?\x{203D}\x{2E18}\x{203C}\x{2047}-\x{2049}\x{3002}]+\s)`)
	reSplitsOnly = regexp.MustCompile(`^[.!?\x{203D}\x{2E18}\x{203C}\x{2047}-\x{2049}\x{3002}]+\s$`)
	reNewLine    = regexp.MustCompile(`((?:\r?\n|\r)+)`)
	reHasLetter  = regexp.MustCompile(`(?i)[a-z0-9\x{00C0}-\x{00FF}\x{00a9}\x{00ae}\x{2000}-\x{3300}\x{d000}-\x{dfff}]`)
	reHasSome    = regexp.MustCompile(`\S`)

	reIsAcronymSent = regexp.MustCompile(`(?i)[ .][A-Z]\.? *$`)
	reHasEllipse    = regexp.MustCompile(`(?:\x{2026}|\.{2,}) *$`)
	reSentLetter    = regexp.MustCompile(`\p{L}`)
	reHasPeriod     = regexp.MustCompile(`\. *$`)
	reLeadInit      = regexp.MustCompile(`^[A-Z]\. $`)
	reTrailPunct    = regexp.MustCompile(`[.!?\x{203D}\x{2E18}\x{203C}\x{2047}-\x{2049}] *$`)
	reStartWS       = regexp.MustCompile(`^\s+`)
)

// splitByRegexCapture mirrors JS String.split with a capturing group, which keeps
// the separators in the result. Go's Split drops them.
func splitByRegexCapture(text string, re *regexp.Regexp) []string {
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if locs == nil {
		return []string{text}
	}
	out := []string{}
	last := 0
	for _, loc := range locs {
		out = append(out, text[last:loc[0]])
		if loc[2] >= 0 {
			out = append(out, text[loc[2]:loc[3]])
		}
		last = loc[1]
	}
	out = append(out, text[last:])
	return out
}

func basicSplit(text string) []string {
	all := []string{}
	for _, line := range splitByRegexCapture(text, reNewLine) {
		arr := splitByRegexCapture(line, reInitSplit)
		for o := 0; o < len(arr); o++ {
			if o+1 < len(arr) && arr[o+1] != "" && reSplitsOnly.MatchString(arr[o+1]) {
				arr[o] += arr[o+1]
				arr[o+1] = ""
			}
			if arr[o] != "" {
				all = append(all, arr[o])
			}
		}
	}
	return all
}

func notEmpty(splits []string) []string {
	chunks := []string{}
	for i := 0; i < len(splits); i++ {
		s := splits[i]
		if s == "" {
			continue
		}
		if !reHasSome.MatchString(s) || !reHasLetter.MatchString(s) {
			if len(chunks) > 0 {
				chunks[len(chunks)-1] += s
				continue
			} else if i+1 < len(splits) && splits[i+1] != "" {
				splits[i+1] = s + splits[i+1]
				continue
			}
		}
		chunks = append(chunks, s)
	}
	return chunks
}

func isSentence(str string) bool {
	if !reSentLetter.MatchString(str) {
		return false
	}
	if reIsAcronymSent.MatchString(str) {
		return false
	}
	if len(str) == 3 && reLeadInit.MatchString(str) {
		return false
	}
	if reHasEllipse.MatchString(str) {
		return false
	}
	txt := reTrailPunct.ReplaceAllString(str, "")
	words := strings.Split(txt, " ")
	lastWord := strings.ToLower(words[len(words)-1])
	if _, ok := abbreviations[lastWord]; ok && reHasPeriod.MatchString(str) {
		return false
	}
	return true
}

func smartMerge(chunks []string) []string {
	sentences := []string{}
	for i := 0; i < len(chunks); i++ {
		c := chunks[i]
		if i+1 < len(chunks) && !isSentence(c) && !strings.HasSuffix(c, "\n") {
			chunks[i+1] = c + chunks[i+1]
		} else if c != "" {
			sentences = append(sentences, c)
			chunks[i] = ""
		}
	}
	return sentences
}

var quotePairs = map[rune]rune{
	'"': '"', '＂': '＂', '“': '”', '‟': '”',
	'„': '”', '⹂': '”', '‚': '’', '«': '»',
	'‹': '›', '‵': '′', '‶': '″', '‷': '‴',
	'〝': '〞', '〟': '〞',
}

const maxQuote = 280

func countRunesIn(s string, set map[rune]bool) int {
	n := 0
	for _, r := range s {
		if set[r] {
			n++
		}
	}
	return n
}

func openQuoteSet() map[rune]bool {
	m := map[rune]bool{}
	for k := range quotePairs {
		m[k] = true
	}
	return m
}

func closeQuoteSet() map[rune]bool {
	m := map[rune]bool{}
	for _, v := range quotePairs {
		m[v] = true
	}
	return m
}

func quoteMerge(splits []string) []string {
	opens, closes := openQuoteSet(), closeQuoteSet()
	closesQuote := func(s string) bool { return s != "" && countRunesIn(s, closes) == 1 }
	arr := []string{}
	for i := 0; i < len(splits); i++ {
		if countRunesIn(splits[i], opens) == 1 {
			if i+1 < len(splits) && closesQuote(splits[i+1]) && len(splits[i+1]) < maxQuote {
				splits[i] += splits[i+1]
				arr = append(arr, splits[i])
				splits[i+1] = ""
				i++
				continue
			}
			if i+2 < len(splits) && closesQuote(splits[i+2]) {
				toAdd := splits[i+1] + splits[i+2]
				if len(toAdd) < maxQuote {
					splits[i] += toAdd
					arr = append(arr, splits[i])
					splits[i+1] = ""
					splits[i+2] = ""
					i += 2
					continue
				}
			}
		}
		arr = append(arr, splits[i])
	}
	return arr
}

const maxParenLen = 250

func parensMerge(splits []string) []string {
	arr := []string{}
	for i := 0; i < len(splits); i++ {
		if strings.Count(splits[i], "(") == 1 {
			if i+1 < len(splits) && len(splits[i+1]) < maxParenLen {
				if strings.Count(splits[i+1], ")") == 1 && !strings.Contains(splits[i+1], "(") {
					splits[i] += splits[i+1]
					arr = append(arr, splits[i])
					splits[i+1] = ""
					i++
					continue
				}
			}
		}
		arr = append(arr, splits[i])
	}
	return arr
}

func splitSentences(text string) []string {
	if text == "" || !reHasSome.MatchString(text) {
		return nil
	}
	// JS replace without /g only rewrites the first occurrence
	text = strings.Replace(text, " ", " ", 1)
	sentences := notEmpty(basicSplit(text))
	sentences = smartMerge(sentences)
	sentences = quoteMerge(sentences)
	sentences = parensMerge(sentences)
	if len(sentences) == 0 {
		return []string{text}
	}
	for i := 1; i < len(sentences); i++ {
		if ws := reStartWS.FindString(sentences[i]); ws != "" {
			sentences[i-1] += ws
			sentences[i] = strings.TrimLeft(sentences[i], " \t\n\r\v\f ")
		}
	}
	return sentences
}

var (
	reHyphenSplit  = regexp.MustCompile(`[-–—]`)
	reHyphenLetNum = regexp.MustCompile("(?i)^([a-zÀ-ÿ`\"'/]+)[-–—]([a-z0-9À-ÿ].*)")
	reHyphenNumLet = regexp.MustCompile("(?i)^[('\"]?([0-9]{1,4})[-–—]([a-zÀ-ÿ`\"'/-]+[)'\"]?$)")
	reIsAlpha      = regexp.MustCompile(`(?i)[a-z]`)
	reTrailBang    = regexp.MustCompile(`[.?!]$`)
)

func hasHyphen(str string) bool {
	parts := reHyphenSplit.Split(str, -1)
	if len(parts) <= 1 {
		return false
	}
	if len([]rune(parts[0])) == 1 && reIsAlpha.MatchString(parts[0]) {
		return false
	}
	if _, ok := wordPrefixes[parts[0]]; ok {
		return false
	}
	second := reTrailBang.ReplaceAllString(strings.TrimSpace(parts[1]), "")
	if _, ok := wordSuffixes[second]; ok {
		return false
	}
	return reHyphenLetNum.MatchString(str) || reHyphenNumLet.MatchString(str)
}

func splitHyphens(word string) []string {
	hyphens := reHyphenSplit.Split(word, -1)
	whichDash := "-"
	if found := reHyphenSplit.FindString(word); found != "" {
		whichDash = found
	}
	arr := make([]string, 0, len(hyphens))
	for o := 0; o < len(hyphens); o++ {
		if o == len(hyphens)-1 {
			arr = append(arr, hyphens[o])
		} else {
			arr = append(arr, hyphens[o]+whichDash)
		}
	}
	return arr
}

var (
	reIsSlash    = regexp.MustCompile(`\p{L} ?/ ?\p{L}+$`)
	reStartRange = regexp.MustCompile(`^[0-9]{1,4}(:[0-9][0-9])?([a-z]{1,2})? ?[-–—] ?$`)
	reEndRange   = regexp.MustCompile(`^[0-9]{1,4}([a-z]{1,2})? ?$`)
	reWordlike   = regexp.MustCompile(`\S`)
	reIsBoundary = regexp.MustCompile(`^[!?.]+$`)
	reNaiive     = regexp.MustCompile(`(\S+)`)
)

var notWord = map[string]bool{
	".": true, "?": true, "!": true, ":": true, ";": true, "-": true, "–": true,
	"—": true, "--": true, "...": true, "(": true, ")": true, "[": true, "]": true,
	`"`: true, "'": true, "`": true, "«": true, "»": true, "*": true, "•": true,
}

func combineSlashes(arr []string) []string {
	for i := 1; i < len(arr)-1; i++ {
		if arr[i] != "" && reIsSlash.MatchString(arr[i]) {
			arr[i-1] += arr[i] + arr[i+1]
			arr[i] = ""
			arr[i+1] = ""
		}
	}
	return arr
}

func combineRanges(arr []string) []string {
	for i := 0; i < len(arr)-1; i++ {
		if arr[i+1] != "" && reStartRange.MatchString(arr[i]) && reEndRange.MatchString(arr[i+1]) {
			arr[i] += arr[i+1]
			arr[i+1] = ""
		}
	}
	return arr
}

func splitWords(str string) []string {
	arr := []string{}
	for _, w := range splitByRegexCapture(str, reNaiive) {
		if hasHyphen(w) {
			arr = append(arr, splitHyphens(w)...)
			continue
		}
		arr = append(arr, w)
	}
	result := []string{}
	carry := ""
	for _, word := range arr {
		if reWordlike.MatchString(word) && !notWord[word] && !reIsBoundary.MatchString(word) {
			if len(result) > 0 {
				result[len(result)-1] += carry
				result = append(result, word)
			} else {
				result = append(result, carry+word)
			}
			carry = ""
		} else {
			carry += word
		}
	}
	if carry != "" {
		if len(result) == 0 {
			result = append(result, "")
		}
		result[len(result)-1] += carry
	}
	result = combineSlashes(result)
	result = combineRanges(result)
	out := result[:0]
	for _, s := range result {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

var (
	reTokLetter   = regexp.MustCompile(`\p{L}`)
	reTokNumber   = regexp.MustCompile(`[\p{N}\p{Sc}]`)
	reHasAcronym  = regexp.MustCompile(`(?i)^[a-z]\.([a-z]\.)+`)
	reChillin     = regexp.MustCompile(`[sn]['’]$`)
	reIsFullNum   = regexp.MustCompile(`^[(+\-]?\d+(th|st|nd|rd)?[)+\-]?$`)
	reTrailSpaces = regexp.MustCompile(` *$`)
)

// parseTerm splits leading/trailing punctuation off a word into pre/post.
func parseTerm(txt string) (str, pre, post string) {
	original := txt
	if _, ok := emoticons[strings.TrimSpace(txt)]; ok {
		return strings.TrimSpace(txt), "", " "
	}
	chars := []rune(txt)
	trimmed := strings.TrimSpace(txt)

	for i, n := 0, len(chars); i < n; i++ {
		if len(chars) == 0 {
			break
		}
		c := chars[0]
		if _, ok := prePunctuation[string(c)]; ok {
			continue
		}
		if (c == '+' || c == '-' || c == '(') && reIsFullNum.MatchString(trimmed) {
			break
		}
		// mirrors compromise's own dead branch: c.length is always 1 there
		if c == '\'' && false {
			break
		}
		if reTokLetter.MatchString(string(c)) || reTokNumber.MatchString(string(c)) {
			break
		}
		pre += string(c)
		chars = chars[1:]
	}

	for i, n := 0, len(chars); i < n; i++ {
		if len(chars) == 0 {
			break
		}
		c := chars[len(chars)-1]
		if _, ok := postPunctuation[string(c)]; ok {
			continue
		}
		if reTokLetter.MatchString(string(c)) || reTokNumber.MatchString(string(c)) {
			break
		}
		if c == '.' && reHasAcronym.MatchString(original) {
			continue
		}
		if c == '\'' && reChillin.MatchString(original) {
			continue
		}
		if (c == '+' || c == ')') && reIsFullNum.MatchString(trimmed) {
			break
		}
		post = string(c) + post
		chars = chars[:len(chars)-1]
	}

	str = string(chars)
	if str == "" {
		post = reTrailSpaces.FindString(original)
		str = reTrailSpaces.ReplaceAllString(original, "")
		pre = ""
	}
	return str, pre, post
}

var (
	reCleanTrailPunct = regexp.MustCompile(`[,;.!?]+$`)
	reEllipses        = regexp.MustCompile(`\x{2026}`)
	reEnDash          = regexp.MustCompile(`\x{2013}`)
	reStartColon      = regexp.MustCompile(`^[:;]`)
	reDots3           = regexp.MustCompile(`\.{3,}$`)
	reTrailGram       = regexp.MustCompile(`[",.!:;?)]+$`)
	reLeadGram        = regexp.MustCompile(`^['"(]+`)
	reZeroWidth       = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}]`)
	reNumComma        = regexp.MustCompile(`([0-9]),([0-9])`)
)

func cleanNormal(str string) string {
	str = strings.ToLower(strings.TrimSpace(str))
	original := str
	str = reCleanTrailPunct.ReplaceAllString(str, "")
	str = reEllipses.ReplaceAllString(str, "...")
	str = reEnDash.ReplaceAllString(str, "-")
	if !reStartColon.MatchString(str) {
		str = reDots3.ReplaceAllString(str, "")
		str = reTrailGram.ReplaceAllString(str, "")
		str = reLeadGram.ReplaceAllString(str, "")
	}
	str = reZeroWidth.ReplaceAllString(str, "")
	str = strings.TrimSpace(str)
	if str == "" {
		str = original
	}
	for {
		next := reNumComma.ReplaceAllString(str, "$1$2")
		if next == str {
			break
		}
		str = next
	}
	return str
}

var (
	rePeriodAcronym    = regexp.MustCompile(`([A-Z]\.)+[A-Z]?,?$`)
	reOneLetterAcronym = regexp.MustCompile(`^[A-Z]\.,?$`)
	reNoPeriodAcronym  = regexp.MustCompile(`[A-Z]{2,}('s|,)?$`)
	reLowerAcronym     = regexp.MustCompile(`([a-z]\.)+[a-z]\.?$`)
)

func isAcronymStr(str string) bool {
	return rePeriodAcronym.MatchString(str) || reLowerAcronym.MatchString(str) ||
		reOneLetterAcronym.MatchString(str) || reNoPeriodAcronym.MatchString(str)
}

func doAcronym(str string) string {
	if isAcronymStr(str) {
		return strings.ReplaceAll(str, ".", "")
	}
	return str
}

// killUnicode transliterates accented latin to ascii, as compromise does before
// lexicon lookup. Text with no mapped runes is returned unchanged.
func killUnicode(str string) string {
	hasUnicode := false
	for _, r := range str {
		if r > unicode.MaxASCII {
			hasUnicode = true
			break
		}
	}
	if !hasUnicode {
		return str
	}
	var b strings.Builder
	for _, r := range str {
		if repl, ok := unicodeFold[string(r)]; ok {
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeTerm(t *Term) {
	str := cleanNormal(t.Text)
	str = killUnicode(str)
	t.Normal = doAcronym(str)
}

var (
	reApostropheS = regexp.MustCompile(`['’]s$`)
	reSApostrophe = regexp.MustCompile(`s['’]$`)
	reLookin      = regexp.MustCompile(`([aeiou][ktrp])in'$`)
	reMachineDash = regexp.MustCompile(`^\p{L}+-\p{L}+$`)
	reTagMention  = regexp.MustCompile(`^[#@]`)
)

func doMachine(t *Term) {
	str := t.Implicit
	if str == "" {
		str = t.Normal
	}
	if str == "" {
		str = t.Text
	}
	str = reApostropheS.ReplaceAllString(str, "")
	str = reSApostrophe.ReplaceAllString(str, "s")
	str = reLookin.ReplaceAllString(str, "${1}ing")
	if reMachineDash.MatchString(str) {
		str = strings.ReplaceAll(str, "-", "")
	}
	str = reTagMention.ReplaceAllString(str, "")
	if str != t.Normal {
		t.Machine = str
	}
}

var (
	reAliasSlash  = regexp.MustCompile(`/`)
	reAliasDomain = regexp.MustCompile(`(?i)[a-z]\.[a-z]`)
	reAliasMath   = regexp.MustCompile(`[0-9]`)
)

func addAliases(t *Term) {
	str := t.Normal
	if str == "" {
		str = t.Text
	}
	if str == "" {
		str = t.Machine
	}
	if a, ok := aliases[str]; ok {
		t.Alias = append(t.Alias, a)
	}
	if reAliasSlash.MatchString(str) && !reAliasDomain.MatchString(str) && !reAliasMath.MatchString(str) {
		arr := strings.Split(str, "/")
		if len(arr) <= 3 {
			for _, word := range arr {
				if word = strings.TrimSpace(word); word != "" {
					t.Alias = append(t.Alias, word)
				}
			}
		}
	}
}

// tokenize turns text into compromise's sentence/term structure.
func tokenize(text string) [][]*Term {
	doc := [][]*Term{}
	for n, sentence := range splitSentences(text) {
		words := splitWords(sentence)
		terms := make([]*Term, 0, len(words))
		for i, w := range words {
			t := newTerm()
			t.Text, t.Pre, t.Post = parseTerm(w)
			normalizeTerm(t)
			doMachine(t)
			addAliases(t)
			t.Index = [2]int{n, i}
			terms = append(terms, t)
		}
		doc = append(doc, terms)
	}
	return doc
}
