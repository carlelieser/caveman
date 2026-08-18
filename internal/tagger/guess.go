package tagger

import "strings"

var isPluralEnding = map[byte][]string{
	'e': {"mice", "louse", "antennae", "formulae", "nebulae", "vertebrae", "vitae"},
	'i': {"tia", "octopi", "viri", "radii", "nuclei", "fungi", "cacti", "stimuli"},
	'n': {"men"},
	't': {"feet"},
}

var pluralExceptions = map[string]bool{"israelis": true, "menus": true, "logos": true}

var notPlural = []string{
	"bus", "mas", "was", "ias", "xas", "vas", "cis", "lis", "nis", "ois", "ris",
	"sis", "tis", "xis", "aus", "cus", "eus", "fus", "gus", "ius", "lus", "nus",
	"das", "ous", "pus", "rus", "sus", "tus", "xus", "aos", "igos", "ados",
	"ogos", "'s", "ss",
}

func looksPlural(str string) bool {
	if str == "" || len(str) <= 3 {
		return false
	}
	if pluralExceptions[str] {
		return true
	}
	end := str[len(str)-1]
	if suffixes, ok := isPluralEnding[end]; ok {
		for _, suffix := range suffixes {
			if strings.HasSuffix(str, suffix) {
				return true
			}
		}
		return false
	}
	if end != 's' {
		return false
	}
	for _, suffix := range notPlural {
		if strings.HasSuffix(str, suffix) {
			return false
		}
	}
	return true
}

var guessVerb = buildGuessVerb()

func buildGuessVerb() map[string]string {
	groups := map[string][]string{
		"Gerund": {"ing"},
		"Actor":  {"erer"},
		"Infinitive": {
			"ate", "ize", "tion", "rify", "then", "ress", "ify", "age", "nce",
			"ect", "ise", "ine", "ish", "ace", "ash", "ure", "tch", "end", "ack",
			"and", "ute", "ade", "ock", "ite", "ase", "ose", "use", "ive", "int",
			"nge", "lay", "est", "ain", "ant", "ent", "eed", "er", "le", "unk",
			"ung", "upt", "en",
		},
		"PastTense": {"ept", "ed", "lt", "nt", "ew", "ld"},
		"PresentTense": {
			"rks", "cks", "nks", "ngs", "mps", "tes", "zes", "ers", "les", "acks",
			"ends", "ands", "ocks", "lays", "eads", "lls", "els", "ils", "ows",
			"nds", "ays", "ams", "ars", "ops", "ffs", "als", "urs", "lds", "ews",
			"ips", "es", "ts", "ns",
		},
		"Participle": {"ken", "wn"},
	}
	// insertion order matters only where two groups share a suffix; none do
	out := map[string]string{}
	for tense, suffixes := range groups {
		for _, s := range suffixes {
			out[s] = tense
		}
	}
	return out
}

func getTense(str string) string {
	if len(str) >= 3 {
		if tense, ok := guessVerb[str[len(str)-3:]]; ok {
			return tense
		}
	}
	if len(str) >= 2 {
		if tense, ok := guessVerb[str[len(str)-2:]]; ok {
			return tense
		}
	}
	if len(str) >= 1 && str[len(str)-1] == 's' {
		return "PresentTense"
	}
	return ""
}
