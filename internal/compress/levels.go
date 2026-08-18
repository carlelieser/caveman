package compress

import "github.com/carlelieser/caveman/internal/policy"

// Level is how much grammar a request is willing to lose. The TS declares it
// here and has the policy parser import it; in Go the dependency runs the other
// way, because policy is the package with no internal imports at all. Aliasing
// keeps one set of level values in the binary rather than two that must agree.
type Level = policy.Level

const (
	LevelLight    = policy.LevelLight
	LevelModerate = policy.LevelModerate
	LevelCaveman  = policy.LevelCaveman
)

var LevelNames = policy.LevelNames

// removable lists the classes each level may drop, as a total enumeration rather
// than a delta from the level below. Nouns, verbs, numbers, proper nouns,
// predicates and `other` appear in none of them, so no level can remove them.
//
// The sets are nested — light ⊂ moderate ⊂ caveman — which is what makes output
// length non-increasing as the level rises. Changing any membership changes
// output bytes for the same input.
var removable = map[Level]map[WordClass]struct{}{
	LevelLight: {
		ClassDeterminer: {},
	},
	LevelModerate: {
		ClassDeterminer:  {},
		ClassPreposition: {},
		ClassConjunction: {},
		ClassAuxiliary:   {},
		ClassCopula:      {},
		ClassPronoun:     {},
	},
	LevelCaveman: {
		ClassDeterminer:  {},
		ClassPreposition: {},
		ClassConjunction: {},
		ClassAuxiliary:   {},
		ClassCopula:      {},
		ClassPronoun:     {},
		ClassAdverb:      {},
		ClassAdjective:   {},
	},
}

// IsRemovable reports whether level permits dropping a word of this class.
func IsRemovable(level Level, wordClass WordClass) bool {
	_, ok := removable[level][wordClass]
	return ok
}
