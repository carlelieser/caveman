package tagger

// tagSetOrdered is a Set that keeps insertion order, which is what
// `Array.from(term.tags)` gives compromise's json() output.
type tagSetOrdered struct {
	order []string
	index map[string]int
}

func newTags() *tagSetOrdered {
	return &tagSetOrdered{index: map[string]int{}}
}

func (s *tagSetOrdered) has(tag string) bool {
	_, ok := s.index[tag]
	return ok
}

func (s *tagSetOrdered) add(tag string) {
	if _, ok := s.index[tag]; ok {
		return
	}
	s.index[tag] = len(s.order)
	s.order = append(s.order, tag)
}

func (s *tagSetOrdered) delete(tag string) {
	i, ok := s.index[tag]
	if !ok {
		return
	}
	delete(s.index, tag)
	s.order = append(s.order[:i], s.order[i+1:]...)
	for j := i; j < len(s.order); j++ {
		s.index[s.order[j]] = j
	}
}

func (s *tagSetOrdered) clear() {
	s.order = s.order[:0]
	s.index = map[string]int{}
}

func (s *tagSetOrdered) size() int { return len(s.order) }

func (s *tagSetOrdered) list() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Term mirrors one compromise term. Implicit terms come from an expanded
// contraction and carry no text of their own.
type Term struct {
	Text     string
	Pre      string
	Post     string
	Normal   string
	Machine  string
	Implicit string
	Root     string
	Switch   string
	Chunk    string
	Alias    []string
	Tags     *tagSetOrdered
	Index    [2]int
	Frozen   bool
	Dirty    bool
	Conf     float64
}

func newTerm() *Term {
	return &Term{Tags: newTags(), Conf: 1}
}

// TagList returns the tags in compromise's emission order.
func (t *Term) TagList() []string { return t.Tags.list() }

// lookupWord is the string compromise uses for lexicon and match lookups.
func (t *Term) lookupWord() string {
	if t.Machine != "" {
		return t.Machine
	}
	return t.Normal
}

func fastTag(t *Term, tags ...string) {
	if t.Frozen {
		return
	}
	for _, tag := range tags {
		if tag != "" {
			t.Tags.add(tag)
		}
	}
}

func addChunk(t *Term, tag string) {
	if tag == "Noun" || tag == "Verb" {
		t.Chunk = tag
	}
}

// setTag applies one tag, first removing whatever the tag graph says it
// conflicts with, then adding the tag's parents.
func setTag(terms []*Term, tag string, isSafe bool) {
	if tag == "" {
		return
	}
	for _, t := range terms {
		tagTerm(t, tag, isSafe)
	}
}

// setTagMulti supports the '#Noun . #Adjective' form, one tag per term.
func setTagMulti(terms []*Term, tags []string, isSafe bool) {
	for i, t := range terms {
		if i >= len(tags) {
			break
		}
		tag := stripHash(tags[i])
		if tag != "" {
			tagTerm(t, tag, isSafe)
		}
	}
}

func stripHash(tag string) string {
	if len(tag) > 0 && tag[0] == '#' {
		return tag[1:]
	}
	return tag
}

func tagTerm(t *Term, tag string, isSafe bool) {
	if t.Tags.has(tag) || tag == "." {
		return
	}
	if t.Frozen {
		isSafe = true
	}
	if known, ok := tagSet[tag]; ok {
		for _, no := range known.not {
			if isSafe && t.Tags.has(no) {
				return
			}
			t.Tags.delete(no)
		}
		for _, parent := range known.parents {
			t.Tags.add(parent)
			addChunk(t, parent)
		}
	}
	t.Tags.add(tag)
	t.Dirty = true
	addChunk(t, tag)
}

func unTagTerms(terms []*Term, tag string) {
	tag = stripHash(tag)
	for _, t := range terms {
		if t.Frozen {
			continue
		}
		if tag == "*" {
			t.Tags.clear()
			continue
		}
		if known, ok := tagSet[tag]; ok {
			for _, child := range known.children {
				t.Tags.delete(child)
			}
		}
		t.Tags.delete(tag)
	}
}
