package telemetry

import (
	"encoding/json"
	"math"
	"strings"
)

// Usage is token counts as the provider billed them, read from the upstream
// response.
//
// Caveman's own accounting estimates characters locally; these are the numbers
// the invoice is built from. The cache fields say whether a forwarded prefix
// still matched: a read is billed at a fraction of the base rate, a creation at
// a premium, so a prefix Caveman rewrote shows up here as creations replacing
// reads. A nil field is a count the response did not carry, which is a
// different thing from a zero it did.
type Usage struct {
	InputTokens         *int
	OutputTokens        *int
	CacheReadTokens     *int
	CacheCreationTokens *int
}

const (
	keyInput         = "input_tokens"
	keyOutput        = "output_tokens"
	keyCacheRead     = "cache_read_input_tokens"
	keyCacheCreation = "cache_creation_input_tokens"
)

func EmptyUsage() Usage { return Usage{} }

func (u Usage) Any() bool {
	return u.InputTokens != nil || u.OutputTokens != nil ||
		u.CacheReadTokens != nil || u.CacheCreationTokens != nil
}

func readCount(source map[string]any, key string) *int {
	value, ok := source[key]
	if !ok {
		return nil
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	count := int(number)
	return &count
}

func asRecord(value any) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return record
}

func firstPresent(next, current *int) *int {
	if next != nil {
		return next
	}
	return current
}

// merge folds one usage object into what has been seen so far. A streamed
// response reports usage twice — message_start carries the input and cache
// counts with output_tokens still at its initial value, and message_delta
// carries the final output count and nothing else — so a later field only
// overwrites an earlier one when it is actually present.
func merge(into Usage, source map[string]any) Usage {
	return Usage{
		InputTokens:         firstPresent(readCount(source, keyInput), into.InputTokens),
		OutputTokens:        firstPresent(readCount(source, keyOutput), into.OutputTokens),
		CacheReadTokens:     firstPresent(readCount(source, keyCacheRead), into.CacheReadTokens),
		CacheCreationTokens: firstPresent(readCount(source, keyCacheCreation), into.CacheCreationTokens),
	}
}

// UsageFrom reads usage out of one parsed event or response body, wherever it
// sits: top-level for a non-streamed message, and under `message` for the
// message_start event that opens a stream.
func UsageFrom(parsed any, current Usage) Usage {
	root := asRecord(parsed)
	if root == nil {
		return current
	}
	usage := current
	if nested := asRecord(root["message"]); nested != nil {
		if nestedUsage := asRecord(nested["usage"]); nestedUsage != nil {
			usage = merge(usage, nestedUsage)
		}
	}
	if direct := asRecord(root["usage"]); direct != nil {
		usage = merge(usage, direct)
	}
	return usage
}

const dataPrefix = "data:"

// EventParser accumulates SSE text and yields the JSON payload of each complete
// data: line. Events are separated by a blank line and a chunk boundary can
// fall anywhere, so the tail is held back until its newline arrives.
type EventParser struct {
	pending string
}

func (p *EventParser) Push(chunk string) []string {
	p.pending += chunk
	lines := strings.Split(p.pending, "\n")
	// The final element is whatever followed the last newline: an incomplete
	// line that the next chunk continues.
	p.pending = lines[len(lines)-1]
	payloads := []string{}
	for _, line := range lines[:len(lines)-1] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, dataPrefix) {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, dataPrefix))
		if payload != "" {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

// UsageObserver watches a response body go past and picks out the token counts,
// parsing only what it recognizes. It never holds the body: whatever it is fed
// has already been forwarded, so a stream stays a stream.
//
// Both encodings are read by the same observer. A non-streamed body arrives as
// one JSON document with no data: lines, so it is parsed whole once the stream
// ends; a streamed one arrives as data: lines parsed as they complete.
type UsageObserver struct {
	parser   EventParser
	usage    Usage
	plain    strings.Builder
	sawEvent bool
}

func NewUsageObserver() *UsageObserver { return &UsageObserver{usage: EmptyUsage()} }

func (o *UsageObserver) Push(chunk string) {
	for _, payload := range o.parser.Push(chunk) {
		o.sawEvent = true
		var parsed any
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			// A payload that is not JSON carries no usage; the body still went
			// through untouched.
			continue
		}
		o.usage = UsageFrom(parsed, o.usage)
	}
	if !o.sawEvent {
		o.plain.WriteString(chunk)
	}
}

// Current is usage as it stands, callable at any point.
func (o *UsageObserver) Current() Usage {
	if o.sawEvent || o.plain.Len() == 0 {
		return o.usage
	}
	var parsed any
	if err := json.Unmarshal([]byte(o.plain.String()), &parsed); err != nil {
		return o.usage
	}
	return UsageFrom(parsed, o.usage)
}
