package anthropic_test

import (
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/ir"
)

const walkBody = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 1024,
  "system": [
    {"type": "text", "text": "system one"},
    {"type": "text", "text": "system two", "cache_control": {"type": "ephemeral"}}
  ],
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "user one"},
      {"type": "image", "source": {"type": "url", "url": "https://example.com/a.png"}}
    ]},
    {"role": "assistant", "content": [
      {"type": "text", "text": "assistant one"},
      {"type": "tool_use", "id": "toolu_1", "name": "search", "input": {"query": "never touched"}},
      {"type": "thinking", "thinking": "never touched", "signature": "sig"}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_1", "content": [
        {"type": "text", "text": "result one"},
        {"type": "text", "text": "result two"}
      ]}
    ]}
  ]
}`

func request(t *testing.T) ir.Request {
	t.Helper()
	return anthropic.ToIR(parseBody(t, walkBody))
}

func textsFor(t *testing.T, scopes []ir.Scope) []string {
	t.Helper()
	nodes := ir.CollectTextNodes(request(t), scopes)
	texts := make([]string, len(nodes))
	for i, node := range nodes {
		texts[i] = node.Text
	}
	return texts
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

func TestScoping(t *testing.T) {
	cases := []struct {
		name   string
		scopes []ir.Scope
		want   []string
	}{
		{"messages", []ir.Scope{ir.ScopeMessages}, []string{"user one", "assistant one"}},
		{"system", []ir.Scope{ir.ScopeSystem}, []string{"system one", "system two"}},
		{"tool_results", []ir.Scope{ir.ScopeToolResults}, []string{"result one", "result two"}},
		{"all scopes in document order", ir.AllScopes, []string{
			"system one", "system two", "user one", "assistant one", "result one", "result two",
		}},
		{"empty scope list", []ir.Scope{}, []string{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := textsFor(t, test.scopes); !equalStrings(got, test.want) {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestReportsOwningRole(t *testing.T) {
	nodes := ir.CollectTextNodes(request(t), ir.AllScopes)
	want := []ir.Role{ir.RoleSystem, ir.RoleSystem, ir.RoleUser, ir.RoleAssistant, ir.RoleUser, ir.RoleUser}
	for i, node := range nodes {
		if node.Role != want[i] {
			t.Errorf("node %d role = %q, want %q", i, node.Role, want[i])
		}
	}
}

func TestReportsCacheControlPresence(t *testing.T) {
	nodes := ir.CollectTextNodes(request(t), []ir.Scope{ir.ScopeSystem})
	want := []bool{false, true}
	for i, node := range nodes {
		if node.HasCacheControl != want[i] {
			t.Errorf("node %d hasCacheControl = %v, want %v", i, node.HasCacheControl, want[i])
		}
	}
}

func TestNestedToolResultNodeAddress(t *testing.T) {
	nodes := ir.CollectTextNodes(request(t), []ir.Scope{ir.ScopeToolResults})
	want := ir.TextNodePath{Scope: ir.ScopeToolResults, MessageIndex: 2, BlockIndex: 0, ToolResultIndex: 1}
	if nodes[1].Path != want {
		t.Errorf("path = %+v, want %+v", nodes[1].Path, want)
	}
}

func TestDirectMessageNodeHasNoToolResultIndex(t *testing.T) {
	nodes := ir.CollectTextNodes(request(t), []ir.Scope{ir.ScopeMessages})
	want := ir.TextNodePath{Scope: ir.ScopeMessages, MessageIndex: 0, BlockIndex: 0, ToolResultIndex: ir.NoIndex}
	if nodes[0].Path != want {
		t.Errorf("path = %+v, want %+v", nodes[0].Path, want)
	}
}

func TestMapReturnsANewRequestWithoutMutatingTheOriginal(t *testing.T) {
	source := request(t)
	ir.MapTextNodes(source, ir.AllScopes, func(node ir.TextNode) string {
		return strings.ToUpper(node.Text)
	})
	want := []string{"system one", "system two", "user one", "assistant one", "result one", "result two"}
	nodes := ir.CollectTextNodes(source, ir.AllScopes)
	got := make([]string, len(nodes))
	for i, node := range nodes {
		got[i] = node.Text
	}
	if !equalStrings(got, want) {
		t.Errorf("original was mutated: got %q", got)
	}
}

func TestMapAppliesToInScopeNodesOnly(t *testing.T) {
	mapped := ir.MapTextNodes(request(t), []ir.Scope{ir.ScopeSystem}, func(node ir.TextNode) string {
		return strings.ToUpper(node.Text)
	})
	nodes := ir.CollectTextNodes(mapped, ir.AllScopes)
	got := make([]string, len(nodes))
	for i, node := range nodes {
		got[i] = node.Text
	}
	want := []string{"SYSTEM ONE", "SYSTEM TWO", "user one", "assistant one", "result one", "result two"}
	if !equalStrings(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMapRewritesNestedToolResultTextInPlace(t *testing.T) {
	mapped := ir.MapTextNodes(request(t), []ir.Scope{ir.ScopeToolResults}, func(ir.TextNode) string {
		return "compressed"
	})
	body := anthropic.FromIR(mapped)
	messages, _ := body.Get("messages")
	third := messages.(ir.Array)[2].(*ir.Object)
	blocks, _ := third.Get("content")
	result := blocks.(ir.Array)[0].(*ir.Object)
	nested, _ := result.Get("content")
	want := `[{"type":"text","text":"compressed"},{"type":"text","text":"compressed"}]`
	if got := ir.MarshalString(nested); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNeverExposesToolUseThinkingOrOpaqueAsText(t *testing.T) {
	for _, text := range textsFor(t, ir.AllScopes) {
		if text == "never touched" {
			t.Error("a non-text block surfaced as a text node")
		}
	}
}

func TestIdentityMapLeavesNonTextBlocksByteIdentical(t *testing.T) {
	source := parseBody(t, walkBody)
	mapped := ir.MapTextNodes(anthropic.ToIR(source), ir.AllScopes, func(node ir.TextNode) string {
		return node.Text
	})
	want := ir.MarshalString(anthropic.FromIR(anthropic.ToIR(source)))
	if got := ir.MarshalString(anthropic.FromIR(mapped)); got != want {
		t.Errorf("identity map changed the body\n got %s\nwant %s", got, want)
	}
}

func TestPreservesCacheControlOnARewrittenBlock(t *testing.T) {
	mapped := ir.MapTextNodes(request(t), []ir.Scope{ir.ScopeSystem}, func(ir.TextNode) string {
		return "short"
	})
	body := anthropic.FromIR(mapped)
	system, _ := body.Get("system")
	want := `[{"type":"text","text":"short"},{"type":"text","text":"short","cache_control":{"type":"ephemeral"}}]`
	if got := ir.MarshalString(system); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMappedStringFormMessageIsReEmittedAsAString(t *testing.T) {
	source := parseBody(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"original text"}]}`)
	mapped := ir.MapTextNodes(anthropic.ToIR(source), []ir.Scope{ir.ScopeMessages}, func(ir.TextNode) string {
		return "mapped text"
	})
	messages, _ := anthropic.FromIR(mapped).Get("messages")
	want := `[{"role":"user","content":"mapped text"}]`
	if got := ir.MarshalString(messages); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestMappedStringFormToolResultIsReEmittedAsAString(t *testing.T) {
	source := parseBody(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"original"}]}]}`)
	mapped := ir.MapTextNodes(anthropic.ToIR(source), []ir.Scope{ir.ScopeToolResults}, func(ir.TextNode) string {
		return "mapped"
	})
	messages, _ := anthropic.FromIR(mapped).Get("messages")
	blocks, _ := messages.(ir.Array)[0].(*ir.Object).Get("content")
	nested, _ := blocks.(ir.Array)[0].(*ir.Object).Get("content")
	if got, ok := nested.(ir.String); !ok || string(got) != "mapped" {
		t.Errorf("got %v, want the string form", nested)
	}
}

// The pipeline finds the cache breakpoint with CollectTextNodes and then acts on
// it while walking with MapTextNodes, matching the two by position. That only
// works while both visit the same nodes in the same order.
func TestCollectAndMapAgreeOnNodeOrder(t *testing.T) {
	source := request(t)
	collected := ir.CollectTextNodes(source, ir.AllScopes)
	want := make([]string, len(collected))
	for i, node := range collected {
		want[i] = node.Text
	}
	got := []string{}
	ir.MapTextNodes(source, ir.AllScopes, func(node ir.TextNode) string {
		got = append(got, node.Text)
		return node.Text
	})
	if !equalStrings(got, want) {
		t.Errorf("map visited %q, collect visited %q", got, want)
	}
}
