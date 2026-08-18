package policy

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	CompressHeader = "X-Caveman-Compress"
	ScopeHeader    = "X-Caveman-Scope"
	CacheHeader    = "X-Caveman-Cache"
	CountHeader    = "X-Caveman-Count"
)

// CavemanHeaderNames is the set the upstream forwarder strips. Every control
// header Caveman reads must appear here or it leaks to the provider.
var CavemanHeaderNames = []string{CompressHeader, ScopeHeader, CacheHeader, CountHeader}

const offValue = "off"

const onValue = "on"

// Level is how much grammar a request is willing to lose.
type Level string

const (
	LevelLight    Level = "light"
	LevelModerate Level = "moderate"
	LevelCaveman  Level = "caveman"
)

var LevelNames = []Level{LevelLight, LevelModerate, LevelCaveman}

func IsLevel(value string) bool {
	for _, name := range LevelNames {
		if string(name) == value {
			return true
		}
	}
	return false
}

// CacheMode is what to do about text a cache_control breakpoint covers.
type CacheMode string

const (
	CacheIgnore  CacheMode = "ignore"
	CacheRespect CacheMode = "respect"
)

var cacheModeNames = []CacheMode{CacheIgnore, CacheRespect}

// Compressing a cached prefix is the default. The compressed bytes are stable
// across turns and a growing conversation rewrites its tail anyway, so the
// alternative buys an unchanged prefix at the price of never compressing the
// largest part of the request.
const DefaultCacheMode = CacheIgnore

type ScopeName string

const (
	ScopeMessages    ScopeName = "messages"
	ScopeSystem      ScopeName = "system"
	ScopeToolResults ScopeName = "tool_results"
)

var scopeNames = []ScopeName{ScopeMessages, ScopeSystem, ScopeToolResults}

// Every scope. A system prompt and its tool results are routinely larger than
// the conversation they frame, so a default of messages alone leaves most of a
// request untouched. Narrowing is done by naming the scopes to keep.
var defaultScope = scopeNames

type Scope map[ScopeName]bool

// Policy is the compression settings one request asked for.
type Policy struct {
	// Level is empty when compression is off, which is the default.
	Level     Level
	Scope     Scope
	CacheMode CacheMode
	// Count asks for real token counts in the savings report. Off by default:
	// counting costs a BPE pass per node per request, and a chat re-sends its
	// history every turn, so the cost follows conversation size rather than the
	// size of the new turn. Off, savings are reported in characters.
	Count bool
}

func (p Policy) CompressionEnabled() bool { return p.Level != "" }

// Failure names the header that was rejected, so the client is told which of
// its values to fix rather than that something was wrong.
type Failure struct {
	Header string
	Value  string
	Reason string
}

func (f Failure) Error() string {
	return fmt.Sprintf("%s: %s (received %q)", f.Header, f.Reason, f.Value)
}

func buildScope(names []ScopeName) Scope {
	scope := make(Scope, len(scopeNames))
	for _, name := range scopeNames {
		scope[name] = false
	}
	for _, name := range names {
		scope[name] = true
	}
	return scope
}

func isScopeName(value string) bool {
	for _, name := range scopeNames {
		if string(name) == value {
			return true
		}
	}
	return false
}

func acceptedLevels() string {
	names := make([]string, 0, len(LevelNames)+1)
	names = append(names, offValue)
	for _, level := range LevelNames {
		names = append(names, string(level))
	}
	return strings.Join(names, ", ")
}

// header reads a value and reports whether the client sent it at all, because
// an absent header takes the default while an empty one is an error.
func header(headers http.Header, name string) (string, bool) {
	values, ok := headers[http.CanonicalHeaderKey(name)]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// Levels are named, never numeric. A fraction used to mean "drop this share of
// the tokens"; nothing removes a share of a class, so a number is now a
// malformed value rather than a legacy spelling to be mapped onto a level.
func parseCompress(headers http.Header) (Level, *Failure) {
	raw, present := header(headers, CompressHeader)
	if !present {
		return "", nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", &Failure{CompressHeader, raw, "must not be empty"}
	}
	normalized := strings.ToLower(trimmed)
	if normalized == offValue {
		return "", nil
	}
	if !IsLevel(normalized) {
		return "", &Failure{CompressHeader, raw, "must be one of " + acceptedLevels()}
	}
	return Level(normalized), nil
}

func parseScope(headers http.Header) ([]ScopeName, *Failure) {
	raw, present := header(headers, ScopeHeader)
	if !present {
		return defaultScope, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, &Failure{ScopeHeader, raw, "must not be empty"}
	}
	seen := map[string]bool{}
	names := []ScopeName{}
	for _, entry := range strings.Split(trimmed, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, &Failure{ScopeHeader, raw, "must not contain empty members"}
		}
		if !isScopeName(entry) {
			return nil, &Failure{ScopeHeader, raw, fmt.Sprintf("unknown scope member %q", entry)}
		}
		if seen[entry] {
			return nil, &Failure{ScopeHeader, raw, fmt.Sprintf("duplicate scope member %q", entry)}
		}
		seen[entry] = true
		names = append(names, ScopeName(entry))
	}
	return names, nil
}

func parseCacheMode(headers http.Header) (CacheMode, *Failure) {
	raw, present := header(headers, CacheHeader)
	if !present {
		return DefaultCacheMode, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", &Failure{CacheHeader, raw, "must not be empty"}
	}
	normalized := strings.ToLower(trimmed)
	for _, mode := range cacheModeNames {
		if string(mode) == normalized {
			return mode, nil
		}
	}
	names := make([]string, len(cacheModeNames))
	for i, mode := range cacheModeNames {
		names[i] = string(mode)
	}
	return "", &Failure{CacheHeader, raw, "must be one of " + strings.Join(names, ", ")}
}

// parseCount reads the counting switch. Absent means off, which is the default,
// so a client that says nothing pays no tokenizer cost.
func parseCount(headers http.Header) (bool, *Failure) {
	raw, present := header(headers, CountHeader)
	if !present {
		return false, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, &Failure{CountHeader, raw, "must not be empty"}
	}
	switch strings.ToLower(trimmed) {
	case onValue:
		return true, nil
	case offValue:
		return false, nil
	}
	return false, &Failure{CountHeader, raw, "must be one of " + onValue + ", " + offValue}
}

// Parse reads the three control headers. The first rejection wins, so a client
// sending two bad values fixes them one at a time in header order.
func Parse(headers http.Header) (Policy, *Failure) {
	level, failure := parseCompress(headers)
	if failure != nil {
		return Policy{}, failure
	}
	names, failure := parseScope(headers)
	if failure != nil {
		return Policy{}, failure
	}
	cacheMode, failure := parseCacheMode(headers)
	if failure != nil {
		return Policy{}, failure
	}
	count, failure := parseCount(headers)
	if failure != nil {
		return Policy{}, failure
	}
	return Policy{Level: level, Scope: buildScope(names), CacheMode: cacheMode, Count: count}, nil
}
