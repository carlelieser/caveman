package compress_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/compress"
	"github.com/carlelieser/caveman/internal/ir"
)

// These run the whole pipeline over the recorded request bodies rather than
// over single text nodes. What they pin is structural: compression rewrites
// text and touches nothing else, so every non-text block, every identifier and
// every marker comes out of the walk exactly as it went in.

type fixture struct {
	Name string
	Body *ir.Object
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "golden", "fixtures")
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
		fixtures = append(fixtures, fixture{Name: entry.Name, Body: value.(*ir.Object)})
	}
	return fixtures
}

func compressBody(body *ir.Object, level compress.Level) *ir.Object {
	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(body),
		Level:     level,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	})
	return anthropic.FromIR(result.Request)
}

// blocksOfKind collects every content block of one type across every message,
// serialized, so two runs can be compared byte for byte.
func blocksOfKind(body *ir.Object, kind string) []string {
	found := []string{}
	messages, ok := body.Get("messages")
	if !ok {
		return found
	}
	array, ok := messages.(ir.Array)
	if !ok {
		return found
	}
	for _, entry := range array {
		message, ok := entry.(*ir.Object)
		if !ok {
			continue
		}
		content, ok := message.Get("content")
		if !ok {
			continue
		}
		blocks, ok := content.(ir.Array)
		if !ok {
			continue
		}
		for _, raw := range blocks {
			block, ok := raw.(*ir.Object)
			if !ok {
				continue
			}
			if blockType, _ := block.Get("type"); blockType == ir.String(kind) {
				found = append(found, ir.MarshalString(block))
			}
		}
	}
	return found
}

func fieldOf(body *ir.Object, name string) string {
	value, ok := body.Get(name)
	if !ok {
		return ""
	}
	return ir.MarshalString(value)
}

// A block the compressor never sees must come back byte-identical. tool_use
// carries arguments the model will act on, thinking carries a signature that
// stops verifying if a byte moves, and an image is not text at all.
func TestOpaqueBlocksSurviveEveryLevel(t *testing.T) {
	kinds := []string{"tool_use", "thinking", "redacted_thinking", "image"}
	for _, f := range loadFixtures(t) {
		for _, level := range compress.LevelNames {
			t.Run(f.Name+"/"+string(level), func(t *testing.T) {
				compressed := compressBody(f.Body, level)
				for _, kind := range kinds {
					got, want := blocksOfKind(compressed, kind), blocksOfKind(f.Body, kind)
					if !equalStrings(got, want) {
						t.Errorf("%s blocks changed\n got %v\nwant %v", kind, got, want)
					}
				}
				if got, want := fieldOf(compressed, "tools"), fieldOf(f.Body, "tools"); got != want {
					t.Errorf("tools changed\n got %s\nwant %s", got, want)
				}
			})
		}
	}
}

// The API rejects a text block whose text is empty, so a node compression would
// empty keeps its original text instead of being sent as a block that fails.
func TestNoEmptyTextBlockIsEverEmitted(t *testing.T) {
	for _, f := range loadFixtures(t) {
		for _, level := range compress.LevelNames {
			compressed := compressBody(f.Body, level)
			for _, block := range blocksOfKind(compressed, "text") {
				var parsed struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal([]byte(block), &parsed); err != nil {
					t.Fatalf("%s: parsing an emitted text block: %v", f.Name, err)
				}
				if strings.TrimSpace(parsed.Text) == "" {
					t.Errorf("%s [%s]: emitted an empty text block", f.Name, level)
				}
			}
		}
	}
}

// A tool_result is matched to its tool_use by id. Rewriting one would leave the
// pair unmatched, which the API rejects, so the ids and their pairing must be
// exactly what arrived.
func TestToolUseIDsAndTheirPairingSurvive(t *testing.T) {
	for _, f := range loadFixtures(t) {
		compressed := compressBody(f.Body, compress.LevelCaveman)
		gotResults := idsIn(t, blocksOfKind(compressed, "tool_result"), "tool_use_id")
		wantResults := idsIn(t, blocksOfKind(f.Body, "tool_result"), "tool_use_id")
		if !equalStrings(gotResults, wantResults) {
			t.Errorf("%s: tool_use_ids changed\n got %v\nwant %v", f.Name, gotResults, wantResults)
		}
		// An unanswered tool_use is a valid shape, since its result may sit in a
		// turn the request does not carry. What must not change is which ones
		// are answered.
		got := answered(idsIn(t, blocksOfKind(compressed, "tool_use"), "id"), gotResults)
		want := answered(idsIn(t, blocksOfKind(f.Body, "tool_use"), "id"), wantResults)
		if !equalStrings(got, want) {
			t.Errorf("%s: answered tool_use ids changed\n got %v\nwant %v", f.Name, got, want)
		}
	}
}

func idsIn(t *testing.T, blocks []string, field string) []string {
	t.Helper()
	ids := []string{}
	for _, block := range blocks {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(block), &parsed); err != nil {
			t.Fatalf("parsing block: %v", err)
		}
		if value, ok := parsed[field].(string); ok {
			ids = append(ids, value)
		}
	}
	return ids
}

func answered(issued, results []string) []string {
	seen := map[string]struct{}{}
	for _, id := range results {
		seen[id] = struct{}{}
	}
	matched := []string{}
	for _, id := range issued {
		if _, ok := seen[id]; ok {
			matched = append(matched, id)
		}
	}
	return matched
}

// The walk offers text nodes and nothing else. A tool_use input or a thinking
// signature reaching the compressor would mean an opaque block became
// compressible, so these markers must never appear in what is offered.
func TestTheWalkNeverOffersANonTextBlock(t *testing.T) {
	markers := []string{"toolu_", "signature", "San Francisco, CA"}
	for _, f := range loadFixtures(t) {
		for _, node := range ir.CollectTextNodes(anthropic.ToIR(f.Body), ir.AllScopes) {
			for _, marker := range markers {
				if strings.Contains(node.Text, marker) {
					t.Errorf("%s: %q reached the compressor as text: %q", f.Name, marker, node.Text)
				}
			}
		}
	}
}

// A cache_control marker names where the cached prefix ends. Moving one changes
// what upstream bills, so a rewritten block keeps the marker it arrived with.
func TestCacheControlMarkersStayOnTheirBlocks(t *testing.T) {
	for _, f := range loadFixtures(t) {
		compressed := compressBody(f.Body, compress.LevelModerate)
		if got, want := markedBlocks(compressed), markedBlocks(f.Body); got != want {
			t.Errorf("%s: %d blocks carry cache_control, want %d", f.Name, got, want)
		}
	}
}

func markedBlocks(body *ir.Object) int {
	count := 0
	for _, section := range []string{"system", "messages"} {
		value, ok := body.Get(section)
		if !ok {
			continue
		}
		count += countMarkers(value)
	}
	return count
}

func countMarkers(value ir.Value) int {
	switch typed := value.(type) {
	case ir.Array:
		total := 0
		for _, entry := range typed {
			total += countMarkers(entry)
		}
		return total
	case *ir.Object:
		total := 0
		if typed.Has("cache_control") {
			total++
		}
		for _, key := range typed.Keys() {
			nested, _ := typed.Get(key)
			total += countMarkers(nested)
		}
		return total
	default:
		return 0
	}
}

// A request whose only text is protected has nothing to drop, so it must come
// back byte-identical rather than reassembled into an equivalent body.
func TestAFullyProtectedRequestIsByteIdentical(t *testing.T) {
	source := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[{"role":"user",` +
		`"content":"` + "```ts\\nconst value = compute(alpha, beta);\\nreturn value;\\n```" + `"}]}`
	value, err := ir.Unmarshal([]byte(source))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	body := value.(*ir.Object)
	for _, level := range compress.LevelNames {
		if got := ir.MarshalString(compressBody(body, level)); got != source {
			t.Errorf("[%s]: got %s\nwant %s", level, got, source)
		}
	}
}

// The prompt cache matches on the serialized prefix, so the same request must
// compress to the same bytes on every turn.
func TestRepeatedRunsProduceIdenticalBytes(t *testing.T) {
	for _, f := range loadFixtures(t) {
		first := ir.MarshalString(compressBody(f.Body, compress.LevelModerate))
		second := ir.MarshalString(compressBody(f.Body, compress.LevelModerate))
		if first != second {
			t.Errorf("%s: two runs differed\n first %s\nsecond %s", f.Name, first, second)
		}
	}
}

// Under `ignore` a node renders the same however the breakpoints around it
// move. A rule that consulted position would fail this: a rolling breakpoint
// advancing past a node would flip it between its compressed and original text
// and invalidate the cached prefix on that turn.
func TestRenderingIsIndependentOfBreakpointPlacement(t *testing.T) {
	turns := []string{
		"Could you please tell me what the weather is like in San Francisco today?",
		"The weather in San Francisco is currently quite foggy and rather cool.",
		"And could you also tell me about the weather in the city of Oslo?",
		"The weather in Oslo is very cold today with a lot of heavy snow.",
		"Which of the two cities that we discussed is the colder one right now?",
	}
	byTurn := make([][]string, len(turns))
	for count := 1; count <= len(turns); count++ {
		byTurn[count-1] = textsOf(compressBody(conversation(t, turns[:count], true), compress.LevelModerate))
	}
	for node := range turns {
		var first string
		for _, texts := range byTurn {
			if len(texts) <= node {
				continue
			}
			if first == "" {
				first = texts[node]
				continue
			}
			if texts[node] != first {
				t.Errorf("node %d rendered as %q and %q as the breakpoint moved",
					node, first, texts[node])
			}
		}
	}

	// Every turn is compressed, including the cached ones.
	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(conversation(t, turns, true)),
		Level:     compress.LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	})
	if result.Stats.NodesSkipped != 0 || result.Stats.NodesCompressed != len(turns) {
		t.Errorf("skipped=%d compressed=%d, want 0/%d",
			result.Stats.NodesSkipped, result.Stats.NodesCompressed, len(turns))
	}

	// Removing the breakpoint entirely changes nothing.
	withBreakpoint := textsOf(compressBody(conversation(t, turns[:3], true), compress.LevelModerate))
	without := textsOf(compressBody(conversation(t, turns[:3], false), compress.LevelModerate))
	if !equalStrings(without, withBreakpoint) {
		t.Errorf("dropping the breakpoint changed the output\n got %v\nwant %v", without, withBreakpoint)
	}
}

// conversation builds a body whose newest turn optionally carries the cache
// breakpoint, which is how a rolling prefix advances turn by turn.
func conversation(t *testing.T, turns []string, breakpoint bool) *ir.Object {
	t.Helper()
	messages := make([]string, 0, len(turns))
	for index, text := range turns {
		role := "user"
		if index%2 == 1 {
			role = "assistant"
		}
		block := `{"type":"text","text":` + quote(text)
		if breakpoint && index == len(turns)-1 {
			block += `,"cache_control":{"type":"ephemeral"}`
		}
		block += `}`
		messages = append(messages, `{"role":"`+role+`","content":[`+block+`]}`)
	}
	source := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[` +
		strings.Join(messages, ",") + `]}`
	value, err := ir.Unmarshal([]byte(source))
	if err != nil {
		t.Fatalf("parsing conversation: %v", err)
	}
	return value.(*ir.Object)
}

func quote(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

func textsOf(body *ir.Object) []string {
	texts := []string{}
	for _, node := range ir.CollectTextNodes(anthropic.ToIR(body), ir.AllScopes) {
		texts = append(texts, node.Text)
	}
	return texts
}

// Under `respect` the cached prefix is returned exactly as the client sent it,
// the nodes it covers are counted as skipped rather than dropped, and the text
// after the last breakpoint is still compressed.
func TestRespectLeavesTheCachedPrefixByteIdentical(t *testing.T) {
	source := `{"model":"claude-sonnet-4-5","max_tokens":1024,` +
		`"system":[{"type":"text","text":"A long stable preamble that would compress well."},` +
		`{"type":"text","text":"The final stable system block, marked as the cache breakpoint.",` +
		`"cache_control":{"type":"ephemeral"}}],` +
		`"messages":[{"role":"user","content":[{"type":"text",` +
		`"text":"An earlier turn that sits inside the cached prefix region."}]}]}`
	value, err := ir.Unmarshal([]byte(source))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	body := value.(*ir.Object)

	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(body),
		Level:     compress.LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheRespect,
	})
	emitted := anthropic.FromIR(result.Request)

	if got, want := fieldOf(emitted, "system"), fieldOf(body, "system"); got != want {
		t.Errorf("the cached prefix was rewritten\n got %s\nwant %s", got, want)
	}
	// Both system blocks precede the breakpoint; the message follows it.
	if result.Stats.NodesSeen != 3 || result.Stats.NodesSkipped != 2 {
		t.Errorf("seen=%d skipped=%d, want 3/2", result.Stats.NodesSeen, result.Stats.NodesSkipped)
	}
	texts := textsOf(emitted)
	if texts[2] == textsOf(body)[2] {
		t.Error("text after the last breakpoint was not compressed")
	}

	// With no breakpoint anywhere, respect compresses everything.
	bare := strings.Replace(source, `,"cache_control":{"type":"ephemeral"}`, "", 1)
	bareValue, err := ir.Unmarshal([]byte(bare))
	if err != nil {
		t.Fatalf("parsing the uncached body: %v", err)
	}
	uncached := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(bareValue.(*ir.Object)),
		Level:     compress.LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheRespect,
	})
	if uncached.Stats.NodesSkipped != 0 || uncached.Stats.NodesCompressed == 0 {
		t.Errorf("skipped=%d compressed=%d with no breakpoint present",
			uncached.Stats.NodesSkipped, uncached.Stats.NodesCompressed)
	}
}

// Under `ignore` the block carrying the marker is compressed like any other,
// and keeps the marker it arrived with.
func TestIgnoreCompressesTheMarkedBlockAndKeepsItsMarker(t *testing.T) {
	source := `{"model":"claude-sonnet-4-5","max_tokens":1024,` +
		`"system":[{"type":"text","text":"A long stable preamble that would compress well.",` +
		`"cache_control":{"type":"ephemeral"}}],` +
		`"messages":[{"role":"user","content":[{"type":"text",` +
		`"text":"An earlier turn that sits inside the cached prefix region."}]}]}`
	value, err := ir.Unmarshal([]byte(source))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	body := value.(*ir.Object)

	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(body),
		Level:     compress.LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	})
	if result.Stats.NodesSkipped != 0 || result.Stats.NodesCompressed == 0 {
		t.Errorf("skipped=%d compressed=%d under ignore",
			result.Stats.NodesSkipped, result.Stats.NodesCompressed)
	}
	emitted := anthropic.FromIR(result.Request)
	if textsOf(emitted)[0] == textsOf(body)[0] {
		t.Error("the marked block was left uncompressed")
	}
	if markedBlocks(emitted) != 1 {
		t.Errorf("%d blocks carry cache_control, want 1", markedBlocks(emitted))
	}
}

// Stats must account for every node the walk saw, and the level they report is
// the one the request asked for.
func TestStatsAccountForEveryWalkedNode(t *testing.T) {
	source := `{"model":"claude-sonnet-4-5","max_tokens":1024,` +
		`"system":"You are a careful assistant that answers questions about the weather.",` +
		`"messages":[{"role":"user","content":` +
		`"Could you please tell me what the weather is like in San Francisco today?"}]}`
	value, err := ir.Unmarshal([]byte(source))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	body := value.(*ir.Object)

	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(body),
		Level:     compress.LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: compress.CacheIgnore,
	})
	if result.Stats.NodesSeen != 2 || result.Stats.NodesCompressed == 0 {
		t.Errorf("seen=%d compressed=%d, want 2 seen", result.Stats.NodesSeen, result.Stats.NodesCompressed)
	}
	if result.Stats.CharsAfter >= result.Stats.CharsBefore {
		t.Errorf("charsAfter %d did not fall below charsBefore %d",
			result.Stats.CharsAfter, result.Stats.CharsBefore)
	}
	if result.Stats.Level != compress.LevelModerate {
		t.Errorf("stats report level %q", result.Stats.Level)
	}

	// A narrowed scope leaves everything outside it byte-identical and uncounted.
	narrowed := compress.RunPipeline(compress.PipelineRequest{
		Request:   anthropic.ToIR(body),
		Level:     compress.LevelModerate,
		Scopes:    []ir.Scope{ir.ScopeSystem},
		CacheMode: compress.CacheIgnore,
	})
	emitted := anthropic.FromIR(narrowed.Request)
	if fieldOf(emitted, "system") == fieldOf(body, "system") {
		t.Error("the system prompt was left uncompressed under a system-only scope")
	}
	if fieldOf(emitted, "messages") != fieldOf(body, "messages") {
		t.Error("messages were touched under a system-only scope")
	}
	if narrowed.Stats.NodesSeen != 1 {
		t.Errorf("nodesSeen = %d under a system-only scope, want 1", narrowed.Stats.NodesSeen)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
