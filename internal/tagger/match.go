package tagger

import (
	"regexp"
	"strings"
)

type matchState struct {
	t           int
	terms       []*Term
	r           int
	regs        []reg
	groups      map[string]*groupSpan
	startI      int
	phraseLen   int
	inGroup     string
	hasGroupNow bool
}

type groupSpan struct {
	start  int
	length int
}

func (s *matchState) group(termIndex int) *groupSpan {
	if g, ok := s.groups[s.inGroup]; ok {
		return g
	}
	g := &groupSpan{start: termIndex}
	s.groups[s.inGroup] = g
	return g
}

var (
	reStartQuote = regexp.MustCompile(`[\x{0022}\x{FF02}\x{0027}\x{201C}\x{2018}\x{201F}\x{201B}\x{201E}\x{2E42}\x{201A}\x{00AB}\x{2039}\x{2035}\x{2036}\x{2037}\x{301D}\x{0060}\x{301F}]`)
	reEndQuote   = regexp.MustCompile(`[\x{0022}\x{FF02}\x{0027}\x{201D}\x{2019}\x{00BB}\x{203A}\x{2032}\x{2033}\x{2034}\x{301E}\x{00B4}]`)
	reTermHyphen = regexp.MustCompile(`^[-–—]$`)
	reTermDash   = regexp.MustCompile(` [-–—]{1,3} `)
	reIsTitleTM  = regexp.MustCompile(`^\p{Lu}[a-z'\x{00C0}-\x{00FF}]`)
	reIsUpperTM  = regexp.MustCompile(`^\p{Lu}+$`)
)

func termMethod(name string, t *Term) bool {
	switch name {
	case "hasQuote", "hasQuotation":
		return reStartQuote.MatchString(t.Pre) || reEndQuote.MatchString(t.Post)
	case "hasComma":
		return strings.Contains(t.Post, ",")
	case "hasPeriod":
		return strings.Contains(t.Post, ".") && !strings.Contains(t.Post, "...")
	case "hasExclamation":
		return strings.Contains(t.Post, "!")
	case "hasQuestionMark":
		return strings.Contains(t.Post, "?") || strings.Contains(t.Post, "¿")
	case "hasEllipses":
		return strings.Contains(t.Post, "..") || strings.Contains(t.Post, "…")
	case "hasSemicolon":
		return strings.Contains(t.Post, ";")
	case "hasColon":
		return strings.Contains(t.Post, ":")
	case "hasSlash":
		return strings.Contains(t.Text, "/")
	case "hasHyphen":
		return reTermHyphen.MatchString(t.Post) || reTermHyphen.MatchString(t.Pre)
	case "hasDash":
		return reTermDash.MatchString(t.Post) || reTermDash.MatchString(t.Pre)
	case "hasContraction":
		return t.Implicit != ""
	case "isAcronym":
		return t.Tags.has("Acronym")
	case "isKnown":
		return t.Tags.size() > 0
	case "isTitleCase", "hasTitleCase":
		return reIsTitleTM.MatchString(t.Text)
	case "isUpperCase":
		return reIsUpperTM.MatchString(t.Text)
	}
	return false
}

// doesMatch is compromise's term-level test, with the negative wrapper applied.
func doesMatch(t *Term, r *reg, index, length int) bool {
	res := doesMatchInner(t, r, index, length)
	if r.negative {
		return !res
	}
	return res
}

func doesMatchInner(t *Term, r *reg, index, length int) bool {
	if r.anything {
		return true
	}
	if r.start && index != 0 {
		return false
	}
	if r.end && index != length-1 {
		return false
	}
	if r.word != "" {
		if t.Machine != "" && t.Machine == r.word {
			return true
		}
		for _, a := range t.Alias {
			if a == r.word {
				return true
			}
		}
		return r.word == t.Text || r.word == t.Normal
	}
	if r.tag != "" {
		return t.Tags.has(r.tag)
	}
	if r.method != "" {
		return termMethod(r.method, t)
	}
	if r.pattern != "" || r.predicate != "" {
		str := t.Normal
		if r.predicate == "pictographic" {
			return pictographic(str)
		}
		if r.re == nil {
			return false
		}
		return r.re.MatchString(str)
	}
	if r.switchForm != "" {
		return t.Switch == r.switchForm
	}
	if r.fastOr != nil {
		str := t.Root
		if str == "" {
			str = t.Implicit
		}
		if str == "" {
			str = t.Machine
		}
		if str == "" {
			str = t.Normal
		}
		if _, ok := r.fastOr[str]; ok {
			return true
		}
		_, ok := r.fastOr[t.Text]
		return ok
	}
	if r.choices != nil {
		if r.operator == "and" {
			for _, block := range r.choices {
				for i := range block {
					if !doesMatch(t, &block[i], index, length) {
						return false
					}
				}
			}
			return true
		}
		for _, block := range r.choices {
			for i := range block {
				if doesMatch(t, &block[i], index, length) {
					return true
				}
			}
		}
		return false
	}
	return false
}

func doOrBlock(s *matchState, skipN int) int {
	block := &s.regs[s.r]
	wasFound := false
	for c := 0; c < len(block.choices); c++ {
		regs := block.choices[c]
		wasFound = true
		for wIndex := range regs {
			cr := &regs[wIndex]
			extra := 0
			t := s.t + wIndex + skipN + extra
			if t < 0 || t >= len(s.terms) {
				wasFound = false
				break
			}
			foundBlock := doesMatch(s.terms[t], cr, t+s.startI, s.phraseLen)
			if foundBlock && cr.greedy {
				for i := 1; i < len(s.terms); i++ {
					if t+i >= len(s.terms) {
						break
					}
					if doesMatch(s.terms[t+i], cr, s.startI+i, s.phraseLen) {
						extra++
					} else {
						break
					}
				}
			}
			skipN += extra
			if !foundBlock {
				wasFound = false
				break
			}
		}
		if wasFound {
			skipN += len(regs)
			break
		}
	}
	if wasFound && block.greedy {
		return doOrBlock(s, skipN)
	}
	return skipN
}

func doAndBlock(s *matchState) int {
	longest := 0
	r := &s.regs[s.r]
	for _, block := range r.choices {
		allWords := true
		for wIndex := range block {
			tryTerm := s.t + wIndex
			if tryTerm < 0 || tryTerm >= len(s.terms) {
				allWords = false
				break
			}
			if !doesMatch(s.terms[tryTerm], &block[wIndex], tryTerm, s.phraseLen) {
				allWords = false
				break
			}
		}
		if !allWords {
			return 0
		}
		if len(block) > longest {
			longest = len(block)
		}
	}
	return longest
}

func getGreedy(s *matchState, endReg *reg) int {
	base := s.regs[s.r]
	r := base
	r.start = false
	r.end = false
	start := s.t
	for ; s.t < len(s.terms); s.t++ {
		if endReg != nil && doesMatch(s.terms[s.t], endReg, s.startI+s.t, s.phraseLen) {
			return s.t
		}
		count := s.t - start + 1
		if r.hasMax && count == r.max {
			return s.t
		}
		if !doesMatch(s.terms[s.t], &r, s.startI+s.t, s.phraseLen) {
			if r.hasMin && count < r.min {
				return -1
			}
			return s.t
		}
	}
	return s.t
}

func greedyTo(s *matchState, nextReg *reg) int {
	t := s.t
	if nextReg == nil {
		return len(s.terms)
	}
	for ; t < len(s.terms); t++ {
		if doesMatch(s.terms[t], nextReg, s.startI+t, s.phraseLen) {
			return t
		}
	}
	return -1
}

func isEndGreedy(r *reg, s *matchState) bool {
	if r.end && r.greedy {
		if s.startI+s.t < s.phraseLen-1 {
			tmp := *r
			tmp.end = false
			if doesMatch(s.terms[s.t], &tmp, s.startI+s.t, s.phraseLen) {
				return true
			}
		}
	}
	return false
}

func negGreedy(s *matchState, r *reg, nextReg *reg) bool {
	skip := 0
	for t := s.t; t < len(s.terms); t++ {
		if doesMatch(s.terms[t], r, s.startI+s.t, s.phraseLen) {
			break
		}
		if nextReg != nil && doesMatch(s.terms[t], nextReg, s.startI+s.t, s.phraseLen) {
			break
		}
		skip++
		if r.hasMax && skip == r.max {
			break
		}
	}
	if skip == 0 {
		return false
	}
	if r.hasMin && r.min > skip {
		return false
	}
	s.t += skip
	return true
}

func doNegative(s *matchState) bool {
	r := &s.regs[s.r]
	tmp := *r
	tmp.negative = false
	if doesMatch(s.terms[s.t], &tmp, s.startI+s.t, s.phraseLen) {
		return false
	}
	if r.optional && s.r+1 < len(s.regs) {
		nextReg := &s.regs[s.r+1]
		if doesMatch(s.terms[s.t], nextReg, s.startI+s.t, s.phraseLen) {
			s.r++
		} else if nextReg.optional && s.r+2 < len(s.regs) {
			if doesMatch(s.terms[s.t], &s.regs[s.r+2], s.startI+s.t, s.phraseLen) {
				s.r += 2
			}
		}
	}
	if r.greedy {
		var nextReg *reg
		if s.r+1 < len(s.regs) {
			nextReg = &s.regs[s.r+1]
		}
		return negGreedy(s, &tmp, nextReg)
	}
	s.t++
	return true
}

func foundOptional(s *matchState) {
	r := &s.regs[s.r]
	if s.r+1 >= len(s.regs) {
		return
	}
	nextReg := &s.regs[s.r+1]
	term := s.terms[s.t]
	nextRegMatched := doesMatch(term, nextReg, s.startI+s.t, s.phraseLen)
	if r.negative || nextRegMatched {
		if s.t+1 >= len(s.terms) ||
			!doesMatch(s.terms[s.t+1], nextReg, s.startI+s.t, s.phraseLen) {
			s.r++
		}
	}
}

func contractionSkip(s *matchState) {
	term := s.terms[s.t]
	r := &s.regs[s.r]
	if term.Implicit != "" && s.t+1 < len(s.terms) {
		next := s.terms[s.t+1]
		if next.Implicit == "" {
			return
		}
		if r.word == term.Normal {
			s.t++
		}
		if r.method == "hasContraction" {
			s.t++
		}
	}
}

func setGroupSpan(s *matchState, startAt int) {
	r := &s.regs[s.r]
	g := s.group(startAt)
	if s.t > 1 && r.greedy {
		g.length += s.t - startAt
	} else {
		g.length++
	}
}

func simpleMatch(s *matchState) bool {
	r := &s.regs[s.r]
	term := s.terms[s.t]
	startAt := s.t
	if r.optional && s.r+1 < len(s.regs) && r.negative {
		return true
	}
	if r.optional && s.r+1 < len(s.regs) {
		foundOptional(s)
	}
	if term.Implicit != "" && s.t+1 < len(s.terms) {
		contractionSkip(s)
	}
	s.t++
	if r.end && s.t != len(s.terms) && !r.greedy {
		return false
	}
	if r.greedy {
		s.t = getGreedy(s, nextRegOf(s))
		if s.t < 0 {
			return false
		}
		if r.hasMin && r.min > s.t {
			return false
		}
		if r.end && s.startI+s.t != s.phraseLen {
			return false
		}
	}
	if s.hasGroupNow {
		setGroupSpan(s, startAt)
	}
	return true
}

func nextRegOf(s *matchState) *reg {
	if s.r+1 < len(s.regs) {
		return &s.regs[s.r+1]
	}
	return nil
}

type matchResult struct {
	pointer [3]int
	groups  map[string][3]int
}

// tryHere runs a rule's tokens against terms starting at one position.
func tryHere(terms []*Term, regs []reg, startI, phraseLen int) *matchResult {
	if len(terms) == 0 || len(regs) == 0 {
		return nil
	}
	s := &matchState{
		terms:     terms,
		regs:      regs,
		groups:    map[string]*groupSpan{},
		startI:    startI,
		phraseLen: phraseLen,
	}
	for ; s.r < len(regs); s.r++ {
		r := &regs[s.r]
		s.hasGroupNow = r.group != ""
		if s.hasGroupNow {
			s.inGroup = r.group
		} else {
			s.inGroup = ""
		}
		if s.t >= len(s.terms) {
			alive := false
			for _, remain := range regs[s.r:] {
				if !remain.optional {
					alive = true
					break
				}
			}
			if !alive {
				break
			}
			return nil
		}
		if r.anything && r.greedy {
			skipto := greedyTo(s, nextRegOf(s))
			if skipto <= 0 {
				return nil
			}
			if r.hasMin && skipto-s.t < r.min {
				return nil
			}
			if r.hasMax && skipto-s.t > r.max {
				s.t += r.max
				continue
			}
			if s.hasGroupNow {
				g := s.group(s.t)
				g.length = skipto - s.t
			}
			s.t = skipto
			continue
		}
		if r.choices != nil && r.operator == "or" {
			skipNum := doOrBlock(s, 0)
			if skipNum > 0 {
				if r.negative {
					return nil
				}
				if s.hasGroupNow {
					g := s.group(s.t)
					g.length += skipNum
				}
				if r.end && s.t+s.startI+skipNum != s.phraseLen {
					return nil
				}
				s.t += skipNum
				continue
			} else if !r.optional {
				return nil
			}
			continue
		}
		if r.choices != nil && r.operator == "and" {
			skipNum := doAndBlock(s)
			if skipNum > 0 {
				if r.negative {
					return nil
				}
				if s.hasGroupNow {
					g := s.group(s.t)
					g.length += skipNum
				}
				if r.end {
					if s.t+s.startI != s.phraseLen-1 {
						return nil
					}
				}
				s.t += skipNum
				continue
			} else if !r.optional {
				return nil
			}
			continue
		}
		if r.anything {
			if r.negative {
				return nil
			}
			if !simpleMatch(s) {
				return nil
			}
			continue
		}
		if isEndGreedy(r, s) {
			if !simpleMatch(s) {
				return nil
			}
			continue
		}
		if r.negative {
			if !doNegative(s) {
				return nil
			}
			continue
		}
		if doesMatch(s.terms[s.t], r, s.startI+s.t, s.phraseLen) {
			if !simpleMatch(s) {
				return nil
			}
			continue
		}
		if r.optional {
			continue
		}
		return nil
	}
	if startI == s.t+startI {
		return nil
	}
	res := &matchResult{pointer: [3]int{0, startI, s.t + startI}, groups: map[string][3]int{}}
	for k, g := range s.groups {
		start := startI + g.start
		res.groups[k] = [3]int{0, start, start + g.length}
	}
	return res
}

func notIfMatches(terms []*Term, not []reg) bool {
	for i := range terms {
		slice := terms[i:]
		if tryHere(slice, not, i, len(terms)) != nil {
			return true
		}
	}
	return false
}

// runMatch applies one rule across a clause, returning the pointers it matched.
func runMatch(terms []*Term, rule *matchRule) [][3]int {
	regs := rule.regs
	if len(regs) == 0 {
		return nil
	}
	minLength := 0
	for _, r := range regs {
		if !r.optional && !r.negative {
			minLength++
		}
	}
	results := []*matchResult{}
	if regs[0].start {
		if res := tryHere(terms, regs, 0, len(terms)); res != nil {
			results = append(results, res)
		}
	} else {
		for i := 0; i < len(terms); i++ {
			slice := terms[i:]
			if len(slice) < minLength {
				break
			}
			if res := tryHere(slice, regs, i, len(terms)); res != nil {
				results = append(results, res)
				end := res.pointer[2]
				if abs(end-1) > i {
					i = abs(end - 1)
				}
			}
		}
	}
	if len(regs) > 0 && regs[len(regs)-1].end {
		kept := results[:0]
		for _, res := range results {
			if res.pointer[2] == len(terms) {
				kept = append(kept, res)
			}
		}
		results = kept
	}
	if rule.notIf != nil {
		kept := results[:0]
		for _, res := range results {
			if !notIfMatches(terms[res.pointer[1]:res.pointer[2]], rule.notIf) {
				kept = append(kept, res)
			}
		}
		results = kept
	}
	ptrs := [][3]int{}
	for _, res := range results {
		if rule.hasGroup {
			if g, ok := res.groups[rule.group]; ok {
				ptrs = append(ptrs, g)
			}
			continue
		}
		ptrs = append(ptrs, res.pointer)
	}
	return ptrs
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// cacheClause collects the words and tags in a clause, which is what lets a rule
// be skipped without running it.
func cacheClause(terms []*Term) map[string]struct{} {
	set := map[string]struct{}{}
	for _, t := range terms {
		if t.Normal != "" {
			set[t.Normal] = struct{}{}
		}
		if t.Machine != "" {
			set[t.Machine] = struct{}{}
		}
		if t.Implicit != "" {
			set[t.Implicit] = struct{}{}
		}
		if t.Root != "" {
			set[t.Root] = struct{}{}
		}
		if t.Switch != "" {
			set["%"+t.Switch+"%"] = struct{}{}
		}
		for _, tag := range t.Tags.list() {
			set["#"+tag] = struct{}{}
		}
	}
	return set
}

func ruleApplies(rule *matchRule, cache map[string]struct{}, termCount int) bool {
	if termCount < rule.minWords {
		return false
	}
	for _, need := range rule.needs {
		if _, ok := cache[need]; !ok {
			return false
		}
	}
	for _, no := range rule.ifNo {
		if _, ok := cache[no]; ok {
			return false
		}
	}
	if len(rule.wants) > 0 {
		found := 0
		for _, want := range rule.wants {
			if _, ok := cache[want]; ok {
				found++
			}
		}
		if found < rule.minWant {
			return false
		}
	}
	return true
}

// postTagger runs the context rules over each comma-split clause.
//
// Rules are gathered in hook order, not rule-list order: compromise's getHooks
// walks the hook map and concatenates each bucket, so a rule hooked on an early
// key runs before a lower-indexed rule hooked on a later one. 'read back' turns on
// this — charge-back (rule 83) has to apply after look-what (rule 481), and
// applying by rule index would leave 'back' Imperative instead of Adverb.
//
// Every rule is matched before any tag is written, because compromise sweeps in
// two phases: bulkMatch collects the hits, then bulkTagger applies them.
func postTagger(doc [][]*Term) {
	for _, clause := range quickSplit(doc) {
		if len(clause) == 0 {
			continue
		}
		cache := cacheClause(clause)
		candidates := candidateRules(cache)

		type todo struct {
			rule *matchRule
			ptr  [3]int
		}
		found := []todo{}
		for _, idx := range candidates {
			rule := &matchRules[idx]
			if !ruleApplies(rule, cache, len(clause)) {
				continue
			}
			for _, ptr := range runMatch(clause, rule) {
				if ptr[1] < 0 || ptr[2] > len(clause) || ptr[1] >= ptr[2] {
					continue
				}
				found = append(found, todo{rule: rule, ptr: ptr})
			}
		}
		for _, t := range found {
			hit := clause[t.ptr[1]:t.ptr[2]]
			applyTags(hit, t.rule.tags)
			for _, tag := range t.rule.unTag {
				unTagTerms(hit, tag)
			}
		}
	}
}

// candidateRules lists the rules whose hook keys the clause satisfies, in hook
// order, keeping the first occurrence of each match string.
func candidateRules(cache map[string]struct{}) []int {
	out := []int{}
	seen := map[string]struct{}{}
	for _, bucket := range hookOrder {
		if _, ok := cache[bucket.key]; !ok {
			continue
		}
		for _, idx := range bucket.rules {
			match := matchRules[idx].match
			if _, dup := seen[match]; dup {
				continue
			}
			seen[match] = struct{}{}
			out = append(out, idx)
		}
	}
	for _, idx := range alwaysRules {
		match := matchRules[idx].match
		if _, dup := seen[match]; dup {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, idx)
	}
	return out
}

// applyTags supports both a single tag and the '#Noun . #Adjective' per-term form.
// A bare Noun also settles the last term's number, as bulkTagger does.
func applyTags(terms []*Term, tags []string) {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if strings.Contains(tag, " ") {
			setTagMulti(terms, strings.Fields(tag), false)
			continue
		}
		tag = stripHash(tag)
		setTag(terms, tag, false)
		if tag == "Noun" && len(terms) > 0 {
			last := terms[len(terms)-1]
			if looksPlural(last.Text) {
				setTag([]*Term{last}, "Plural", false)
			} else {
				setTag([]*Term{last}, "Singular", false)
			}
		}
	}
}
