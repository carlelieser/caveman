package cli

import "os"

// Counting real tokens costs a BPE pass over every node's text, on both sides of
// the compression, for every request. A chat re-sends its history each turn, so
// that cost follows the size of the conversation rather than the size of the new
// turn. It is off unless asked for, and off the savings report is in characters.
const DefaultCount = false

// countFlag is the spelling on the command line. Unlike --level it takes no
// value: presence is the answer, and --no-count is how a stored yes is turned
// back off for one command.
const (
	countFlag   = "--count"
	noCountFlag = "--no-count"
)

// storedCount spellings, which are also what the file holds.
const (
	countOn  = "on"
	countOff = "off"
)

func (p Paths) readStoredCount() (bool, bool) {
	contents, err := os.ReadFile(p.CountFile)
	if err != nil {
		return false, false
	}
	switch string(contents) {
	case countOn:
		return true, true
	case countOff:
		return false, true
	}
	return false, false
}

func (p Paths) storeCount(count bool) *exitError {
	if failure := p.ensureRunDir(); failure != nil {
		return failure
	}
	value := countOff
	if count {
		value = countOn
	}
	if err := os.WriteFile(p.CountFile, []byte(value), 0o644); err != nil {
		return die(ExitFailure, "writing the counting setting to %s failed", p.CountFile)
	}
	return nil
}

// resolveCount: the flag on this command wins; otherwise the one `up` stored;
// otherwise off. Mirrors resolveLevel, so counting is inherited the same way a
// level is.
func (p Paths) resolveCount(flag *bool) bool {
	if flag != nil {
		return *flag
	}
	if stored, ok := p.readStoredCount(); ok {
		return stored
	}
	return DefaultCount
}
