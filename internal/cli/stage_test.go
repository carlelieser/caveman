package cli_test

import (
	"net/http"
	"testing"

	"github.com/carlelieser/caveman/internal/cli"
	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
)

// CompressionStage is where a parsed header becomes a pipeline argument. The
// server tests stub the stage out to stay provider-neutral and the compress
// tests call the pipeline directly, so the translation between the two is only
// covered here: a scope the policy turned off must not be walked, and a cache
// mode the header named must reach the pipeline rather than the default.

const stageText = "The man went to the store and he bought some of the bread."

func stagePolicy(t *testing.T, entries map[string]string) policy.Policy {
	t.Helper()
	headers := http.Header{}
	for name, value := range entries {
		headers.Set(name, value)
	}
	parsed, failure := policy.Parse(headers)
	if failure != nil {
		t.Fatalf("parsing %v: %v", entries, failure)
	}
	return parsed
}

func stageRequest(marked ...bool) ir.Request {
	block := func(index int) ir.Content {
		text := &ir.TextContent{Text: stageText}
		if index < len(marked) && marked[index] {
			text.CacheControl = ir.NewObject()
		}
		return text
	}
	return ir.Request{
		HasSystem: true,
		System:    []ir.Content{block(0)},
		Messages:  []ir.Message{{Role: ir.RoleUser, Content: []ir.Content{block(1)}}},
	}
}

func stageTexts(request ir.Request) []string {
	texts := []string{}
	for _, node := range ir.CollectTextNodes(request, ir.AllScopes) {
		texts = append(texts, node.Text)
	}
	return texts
}

// A request that asked for nothing is forwarded without a walk, so it carries
// no stats for the accounting headers to report.
func TestStageForwardsUncompressedWhenTheLevelIsOff(t *testing.T) {
	for _, entries := range []map[string]string{{}, {policy.CompressHeader: "off"}} {
		result := cli.CompressionStage(stageRequest(), stagePolicy(t, entries))
		if result.Stats != nil {
			t.Errorf("%v: reported stats for an uncompressed request", entries)
		}
		for index, text := range stageTexts(result.Request) {
			if text != stageText {
				t.Errorf("%v: node %d was rewritten to %q", entries, index, text)
			}
		}
	}
}

func TestStageCompressesAtTheLevelTheHeaderNamed(t *testing.T) {
	result := cli.CompressionStage(stageRequest(),
		stagePolicy(t, map[string]string{policy.CompressHeader: "moderate"}))
	if result.Stats == nil {
		t.Fatal("no stats for a compressed request")
	}
	if result.Stats.Level != policy.LevelModerate {
		t.Errorf("stats report level %q", result.Stats.Level)
	}
	if result.Stats.NodesSeen != 2 || result.Stats.NodesCompressed != 2 {
		t.Errorf("seen=%d compressed=%d, want 2/2", result.Stats.NodesSeen, result.Stats.NodesCompressed)
	}
	for index, text := range stageTexts(result.Request) {
		if text == stageText {
			t.Errorf("node %d was left uncompressed", index)
		}
	}
}

// A scope the header left out is not walked, so a node inside it is neither
// rewritten nor counted.
func TestStageWalksOnlyTheScopesThePolicyEnabled(t *testing.T) {
	result := cli.CompressionStage(stageRequest(), stagePolicy(t, map[string]string{
		policy.CompressHeader: "moderate",
		policy.ScopeHeader:    "messages",
	}))
	texts := stageTexts(result.Request)
	if texts[0] != stageText {
		t.Errorf("the system node was rewritten to %q under a messages-only scope", texts[0])
	}
	if texts[1] == stageText {
		t.Error("the message node was left uncompressed")
	}
	if result.Stats.NodesSeen != 1 {
		t.Errorf("nodesSeen = %d under a messages-only scope, want 1", result.Stats.NodesSeen)
	}
}

// The cache header is the only way to reach `respect` in a running proxy, and
// the default is `ignore`. Both must arrive at the pipeline as the mode named.
func TestStagePassesTheCacheModeThrough(t *testing.T) {
	// The system block carries the breakpoint, so under respect it and nothing
	// after it in the prefix is compressed, while ignore compresses both.
	marked := stageRequest(true, false)

	respect := cli.CompressionStage(marked, stagePolicy(t, map[string]string{
		policy.CompressHeader: "moderate",
		policy.CacheHeader:    "respect",
	}))
	if respect.Stats.NodesSkipped != 1 {
		t.Errorf("respect skipped %d nodes, want 1", respect.Stats.NodesSkipped)
	}
	if texts := stageTexts(respect.Request); texts[0] != stageText {
		t.Errorf("respect rewrote the cached node to %q", texts[0])
	}

	ignore := cli.CompressionStage(marked, stagePolicy(t, map[string]string{
		policy.CompressHeader: "moderate",
	}))
	if ignore.Stats.NodesSkipped != 0 {
		t.Errorf("the default mode skipped %d nodes, want 0", ignore.Stats.NodesSkipped)
	}
	if texts := stageTexts(ignore.Request); texts[0] == stageText {
		t.Error("the default mode left the cached node uncompressed")
	}
}
