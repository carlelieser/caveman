package tagger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The raw term tags are the secondary gate. A tag difference only matters when it
// changes the word class classify derives, which internal/compress asserts
// exactly; this test reports term-level agreement as a diagnostic.

type goldenTerm struct {
	Text string   `json:"text"`
	Pre  string   `json:"pre"`
	Post string   `json:"post"`
	Tags []string `json:"tags"`
}

type goldenNode struct {
	ID    string       `json:"id"`
	Text  string       `json:"text"`
	Terms []goldenTerm `json:"terms"`
}

func loadNodes(t *testing.T) []goldenNode {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "tagger.json"))
	if err != nil {
		t.Fatalf("read tagger.json: %v", err)
	}
	var nodes []goldenNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		t.Fatalf("parse tagger.json: %v", err)
	}
	return nodes
}

func flatten(text string) []*Term {
	out := []*Term{}
	for _, sentence := range Parse(text) {
		out = append(out, sentence...)
	}
	return out
}

// TestTermSegmentation pins the tokenizer: the text/pre/post split must agree with
// compromise for every term, since classify recovers offsets from exactly these.
func TestTermSegmentation(t *testing.T) {
	nodes := loadNodes(t)
	total, matched := 0, 0
	for _, node := range nodes {
		got := flatten(node.Text)
		if len(got) != len(node.Terms) {
			t.Errorf("%s: got %d terms, want %d", node.ID, len(got), len(node.Terms))
			continue
		}
		for i, want := range node.Terms {
			total++
			if got[i].Text == want.Text && got[i].Pre == want.Pre && got[i].Post == want.Post {
				matched++
				continue
			}
			t.Errorf("%s term %d: got text=%q pre=%q post=%q, want text=%q pre=%q post=%q",
				node.ID, i, got[i].Text, got[i].Pre, got[i].Post, want.Text, want.Pre, want.Post)
		}
	}
	t.Logf("term segmentation: %d/%d terms match", matched, total)
}

// TestTermTags reports raw tag agreement. Tags are compared as sets: compromise
// emits them in Set-insertion order, which carries no meaning for classify.
func TestTermTags(t *testing.T) {
	nodes := loadNodes(t)
	total, matched := 0, 0
	var diffs []string
	for _, node := range nodes {
		got := flatten(node.Text)
		if len(got) != len(node.Terms) {
			continue
		}
		for i, want := range node.Terms {
			total++
			if sameTags(got[i].TagList(), want.Tags) {
				matched++
				continue
			}
			if len(diffs) < 20 {
				diffs = append(diffs, node.ID+" "+want.Text+
					": got ["+strings.Join(got[i].TagList(), " ")+
					"] want ["+strings.Join(want.Tags, " ")+"]")
			}
		}
	}
	t.Logf("term tags: %d/%d terms match (%.2f%%)", matched, total, 100*float64(matched)/float64(total))
	for _, d := range diffs {
		t.Logf("  %s", d)
	}
}

func sameTags(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]int, len(got))
	for _, tag := range got {
		set[tag]++
	}
	for _, tag := range want {
		set[tag]--
		if set[tag] < 0 {
			return false
		}
	}
	return true
}
