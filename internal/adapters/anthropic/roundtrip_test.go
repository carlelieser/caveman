package anthropic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/ir"
)

type fixture struct {
	Name  string
	Bytes []byte
	Body  *ir.Object
}

func fixturesDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "golden", "fixtures"))
	if err != nil {
		t.Fatalf("resolving fixtures dir: %v", err)
	}
	return root
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	dir := fixturesDir(t)
	indexBytes, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("reading fixture index: %v", err)
	}
	var index []struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		t.Fatalf("parsing fixture index: %v", err)
	}

	fixtures := make([]fixture, 0, len(index))
	for _, entry := range index {
		raw, err := os.ReadFile(filepath.Join(dir, entry.File))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.File, err)
		}
		value, err := ir.Unmarshal(raw)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.File, err)
		}
		body, ok := value.(*ir.Object)
		if !ok {
			t.Fatalf("%s: fixture body is not an object", entry.File)
		}
		fixtures = append(fixtures, fixture{Name: entry.Name, Bytes: raw, Body: body})
	}
	return fixtures
}

func roundTrip(body *ir.Object) *ir.Object {
	return anthropic.FromIR(anthropic.ToIR(body))
}

// The gate. Prompt cache lookup matches on the serialized request prefix, so a
// body that is structurally equal but serializes to different bytes misses the
// cache and re-writes every cached segment. Only byte equality can see that.
func TestRoundTripIsByteIdentical(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) != 30 {
		t.Fatalf("expected 30 fixtures, found %d", len(fixtures))
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			got := ir.Marshal(roundTrip(f.Body))
			if string(got) != string(f.Bytes) {
				t.Errorf("round-trip changed the bytes\n%s", byteDiff(f.Bytes, got))
			}
		})
	}
}

func TestRoundTripIsIdempotent(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			got := ir.Marshal(roundTrip(roundTrip(f.Body)))
			if string(got) != string(f.Bytes) {
				t.Errorf("second round-trip changed the bytes\n%s", byteDiff(f.Bytes, got))
			}
		})
	}
}

func TestRoundTripDoesNotMutateTheInput(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			roundTrip(f.Body)
			if got := ir.MarshalString(f.Body); got != string(f.Bytes) {
				t.Errorf("input body was mutated\n%s", byteDiff(f.Bytes, []byte(got)))
			}
		})
	}
}

func parseBody(t *testing.T, text string) *ir.Object {
	t.Helper()
	value, err := ir.Unmarshal([]byte(text))
	if err != nil {
		t.Fatalf("parsing body: %v", err)
	}
	body, ok := value.(*ir.Object)
	if !ok {
		t.Fatalf("body is not an object")
	}
	return body
}

func TestAbsentOptionalFieldsStayAbsent(t *testing.T) {
	body := parseBody(t, `{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	out := roundTrip(body)
	if out.Has("system") {
		t.Error("system reappeared on a request that had none")
	}
	if out.Has("tools") {
		t.Error("tools reappeared on a request that had none")
	}
}

func TestAbsentBlockFlagsStayAbsent(t *testing.T) {
	body := parseBody(t, `{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}]}`)
	out := roundTrip(body)
	messages, _ := out.Get("messages")
	block := messages.(ir.Array)[0].(*ir.Object)
	blocks, _ := block.Get("content")
	first := blocks.(ir.Array)[0].(*ir.Object)
	if first.Has("is_error") {
		t.Error("is_error reappeared on a block that had none")
	}
	if first.Has("cache_control") {
		t.Error("cache_control reappeared on a block that had none")
	}
}

func TestUnknownBlockTypeDegradesToOpaque(t *testing.T) {
	body := parseBody(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"quantum_foo","bar":1,"deep":{"list":[1,2,3]}}]}]}`)
	request := anthropic.ToIR(body)
	block := request.Messages[0].Content[0]
	opaque, ok := block.(*ir.OpaqueContent)
	if !ok {
		t.Fatalf("expected an opaque block, got %T", block)
	}
	if got := ir.MarshalString(opaque.Raw); got != `{"type":"quantum_foo","bar":1,"deep":{"list":[1,2,3]}}` {
		t.Errorf("opaque block was not kept verbatim: %s", got)
	}
}

func TestThinkingBlocksArePreservedWholesale(t *testing.T) {
	body := parseBody(t, `{"model":"m","max_tokens":1,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig"}]}]}`)
	request := anthropic.ToIR(body)
	block := request.Messages[0].Content[0]
	thinking, ok := block.(*ir.ThinkingContent)
	if !ok {
		t.Fatalf("expected a thinking block, got %T", block)
	}
	if got := ir.MarshalString(thinking.Raw); got != `{"type":"thinking","thinking":"reasoning","signature":"sig"}` {
		t.Errorf("thinking block was not kept verbatim: %s", got)
	}
}

// An empty tools array is not modelled, so it must survive as the passthrough
// field it went in as rather than being dropped by the tools-length check.
func TestEmptyToolsArraySurvives(t *testing.T) {
	source := `{"model":"m","max_tokens":1,"tools":[],"messages":[{"role":"user","content":"hi"}]}`
	if got := ir.MarshalString(roundTrip(parseBody(t, source))); got != source {
		t.Errorf("empty tools array did not survive\n%s", byteDiff([]byte(source), []byte(got)))
	}
}

func byteDiff(want, got []byte) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	from := at - 60
	if from < 0 {
		from = 0
	}
	window := func(b []byte) string {
		to := at + 60
		if to > len(b) {
			to = len(b)
		}
		return string(b[from:to])
	}
	return "first difference at byte " + itoa(at) +
		"\n  want ..." + window(want) +
		"\n   got ..." + window(got)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
