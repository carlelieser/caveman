package compress

import (
	"testing"

	"github.com/carlelieser/caveman/internal/ir"
)

func textBlock(text string, hasCacheControl bool) *ir.TextContent {
	block := &ir.TextContent{Text: text}
	if hasCacheControl {
		block.CacheControl = ir.NewObject()
	}
	return block
}

func userRequest(blocks ...ir.Content) ir.Request {
	return ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: blocks}}}
}

func outputTexts(request ir.Request) []string {
	texts := []string{}
	for _, node := range ir.CollectTextNodes(request, ir.AllScopes) {
		texts = append(texts, node.Text)
	}
	return texts
}

const verbose = "The man went to the store and he bought some of the bread."

// TestPipelineCompressesEveryNode covers the default mode, where a breakpoint
// changes nothing and every in-scope node is rewritten.
func TestPipelineCompressesEveryNode(t *testing.T) {
	request := userRequest(textBlock(verbose, true), textBlock(verbose, false))
	result := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: CacheIgnore,
	})
	if result.Stats.NodesSeen != 2 || result.Stats.NodesCompressed != 2 || result.Stats.NodesSkipped != 0 {
		t.Fatalf("seen=%d compressed=%d skipped=%d, want 2/2/0",
			result.Stats.NodesSeen, result.Stats.NodesCompressed, result.Stats.NodesSkipped)
	}
	for index, text := range outputTexts(result.Request) {
		if text == verbose {
			t.Errorf("node %d was left uncompressed", index)
		}
	}
	if result.Stats.CharsAfter >= result.Stats.CharsBefore {
		t.Errorf("charsAfter %d did not fall below charsBefore %d",
			result.Stats.CharsAfter, result.Stats.CharsBefore)
	}
}

// TestPipelineRespectsCachedPrefix covers the older mode: every node at or before
// the last breakpoint is returned verbatim, and counted as skipped rather than
// dropped from the stats.
func TestPipelineRespectsCachedPrefix(t *testing.T) {
	request := userRequest(textBlock(verbose, false), textBlock(verbose, true), textBlock(verbose, false))
	result := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: CacheRespect,
	})
	if result.Stats.NodesSeen != 3 || result.Stats.NodesSkipped != 2 || result.Stats.NodesCompressed != 1 {
		t.Fatalf("seen=%d skipped=%d compressed=%d, want 3/2/1",
			result.Stats.NodesSeen, result.Stats.NodesSkipped, result.Stats.NodesCompressed)
	}
	texts := outputTexts(result.Request)
	if texts[0] != verbose || texts[1] != verbose {
		t.Errorf("cached prefix was rewritten: %q, %q", texts[0], texts[1])
	}
	if texts[2] == verbose {
		t.Errorf("the tail node was not compressed")
	}
	// A skipped node is still classified, so the prose share covers the whole
	// request rather than only the compressible tail.
	if result.Stats.CharsProse != 3*ProseLength(verbose) {
		t.Errorf("charsProse = %d, want %d", result.Stats.CharsProse, 3*ProseLength(verbose))
	}
}

// TestPipelineKeepsEmptiedNode covers the empty-block guard: the API rejects an
// empty text block, so a node compressed to nothing keeps its original text and
// is not counted as compressed.
func TestPipelineKeepsEmptiedNode(t *testing.T) {
	const removableOnly = "the a an"
	request := userRequest(textBlock(removableOnly, false))
	result := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelCaveman,
		Scopes:    ir.AllScopes,
		CacheMode: CacheIgnore,
	})
	if got := outputTexts(result.Request)[0]; got != removableOnly {
		t.Errorf("emptied node became %q, want %q", got, removableOnly)
	}
	if result.Stats.NodesCompressed != 0 {
		t.Errorf("nodesCompressed = %d, want 0", result.Stats.NodesCompressed)
	}
	if result.Stats.CharsAfter != result.Stats.CharsBefore {
		t.Errorf("charsAfter %d != charsBefore %d for a node kept whole",
			result.Stats.CharsAfter, result.Stats.CharsBefore)
	}
}

// TestPipelineScopesLimitTheWalk pins that a node outside the enabled scopes is
// neither rewritten nor counted.
func TestPipelineScopesLimitTheWalk(t *testing.T) {
	request := ir.Request{
		HasSystem: true,
		System:    []ir.Content{textBlock(verbose, false)},
		Messages:  []ir.Message{{Role: ir.RoleUser, Content: []ir.Content{textBlock(verbose, false)}}},
	}
	result := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    []ir.Scope{ir.ScopeMessages},
		CacheMode: CacheIgnore,
	})
	if result.Stats.NodesSeen != 1 {
		t.Fatalf("nodesSeen = %d, want 1", result.Stats.NodesSeen)
	}
	if texts := outputTexts(result.Request); texts[0] != verbose {
		t.Errorf("out-of-scope system node was rewritten to %q", texts[0])
	}
}

// Counting is opt-in, so the default walk must leave the token totals alone
// while still producing the character totals the report falls back on.
func TestPipelineSkipsCountingUnlessAsked(t *testing.T) {
	request := userRequest(textBlock(verbose, false), textBlock(verbose, true))
	uncounted := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: CacheIgnore,
	})
	if uncounted.Stats.TokensBefore != 0 || uncounted.Stats.TokensAfter != 0 {
		t.Errorf("counted %d→%d tokens with counting off",
			uncounted.Stats.TokensBefore, uncounted.Stats.TokensAfter)
	}
	if uncounted.Stats.Counted {
		t.Error("stats claim a tokenizer ran with counting off")
	}
	if uncounted.Stats.CharsBefore == 0 || uncounted.Stats.CharsAfter >= uncounted.Stats.CharsBefore {
		t.Errorf("character totals %d→%d do not show a saving",
			uncounted.Stats.CharsBefore, uncounted.Stats.CharsAfter)
	}

	counted := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: CacheIgnore,
		Count:     true,
	})
	if !counted.Stats.Counted {
		t.Error("stats do not record that a tokenizer ran")
	}
	if counted.Stats.TokensBefore == 0 || counted.Stats.TokensAfter >= counted.Stats.TokensBefore {
		t.Errorf("token totals %d→%d do not show a saving",
			counted.Stats.TokensBefore, counted.Stats.TokensAfter)
	}
	// Counting must not change what the compressor produces, only what is
	// reported about it.
	if counted.Stats.CharsBefore != uncounted.Stats.CharsBefore ||
		counted.Stats.CharsAfter != uncounted.Stats.CharsAfter {
		t.Errorf("counting changed the output: %d→%d counted, %d→%d not",
			counted.Stats.CharsBefore, counted.Stats.CharsAfter,
			uncounted.Stats.CharsBefore, uncounted.Stats.CharsAfter)
	}
}

// A skipped node is charged to both sides, so counting it must not invent a
// saving the cached prefix did not produce.
func TestPipelineCountsSkippedNodesOnBothSides(t *testing.T) {
	request := userRequest(textBlock(verbose, true), textBlock(verbose, false))
	result := RunPipeline(PipelineRequest{
		Request:   request,
		Level:     LevelModerate,
		Scopes:    ir.AllScopes,
		CacheMode: CacheRespect,
		Count:     true,
	})
	if result.Stats.NodesSkipped == 0 {
		t.Fatal("no node was skipped, so this proves nothing")
	}
	if result.Stats.TokensBefore <= result.Stats.TokensAfter {
		t.Errorf("token totals %d→%d do not show the tail's saving",
			result.Stats.TokensBefore, result.Stats.TokensAfter)
	}
}
