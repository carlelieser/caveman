package compress

import (
	"strings"

	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/telemetry"
)

// CacheMode is what to do about text a `cache_control` breakpoint covers. The
// values are the policy parser's, so the header a request carries reaches the
// walk without a translation step in between.
//
// CacheIgnore compresses every in-scope node wherever it sits. The compressor
// reads a node's text and the level, never its position, so a node has one
// compressed form and produces it on every turn. The prefix the cache matches on
// stays stable as the conversation grows.
//
// CacheRespect is the older rule: skip every node at or before the last
// breakpoint. The cached prefix stays byte-identical to the one the client sent
// and is never compressed. It is unstable across turns — a node compressed while
// it sat in the tail is skipped once a rolling breakpoint advances past it, so
// its bytes change and the prefix the mode protects is the one it invalidates.
type CacheMode = policy.CacheMode

const (
	CacheIgnore  = policy.CacheIgnore
	CacheRespect = policy.CacheRespect
)

type PipelineResult struct {
	Request ir.Request
	Stats   telemetry.PipelineStats
}

type PipelineRequest struct {
	Request   ir.Request
	Level     Level
	Scopes    []ir.Scope
	CacheMode CacheMode
}

const defaultRole = RoleUser

func contextOf(node ir.TextNode) CompressContext {
	role := CompressRole(node.Role)
	if node.Role == "" {
		role = defaultRole
	}
	kind := KindText
	if node.Path.Scope == ir.ScopeToolResults {
		kind = KindToolResult
	}
	return CompressContext{Role: role, Kind: kind}
}

// lastCacheBreakpoint is the index of the last node carrying `cache_control`, or
// -1 when none does. Everything up to and including that node is part of a cached
// prefix.
//
// Only consulted under CacheRespect. Note that a breakpoint on a non-text block —
// a `tool_result` or a `tool_use` — is invisible here, because the walk yields
// only text nodes.
func lastCacheBreakpoint(nodes []ir.TextNode) int {
	last := -1
	for index, node := range nodes {
		if node.HasCacheControl {
			last = index
		}
	}
	return last
}

// cachedThroughIndex is the nodes to leave verbatim, as an index bound. -1 leaves
// none.
func cachedThroughIndex(request PipelineRequest) int {
	if request.CacheMode != CacheRespect {
		return -1
	}
	return lastCacheBreakpoint(ir.CollectTextNodes(request.Request, request.Scopes))
}

// skipNode returns a node inside the cached prefix verbatim, but still counts it.
// It is classified anyway, so the prose share covers the whole request rather
// than only the compressible tail.
func skipNode(node ir.TextNode, stats *telemetry.PipelineStats) string {
	stats.NodesSeen++
	stats.NodesSkipped++
	stats.CharsBefore += len(node.Text)
	stats.CharsAfter += len(node.Text)
	stats.CharsProse += ProseLength(node.Text)
	return node.Text
}

func isWhitespaceOnly(text string) bool {
	return strings.TrimFunc(text, isJSSpace) == ""
}

// hasEmptied reports the case the API rejects: an emptied text block. Such a node
// keeps its text.
func hasEmptied(before, after string) bool {
	return isWhitespaceOnly(after) && !isWhitespaceOnly(before)
}

func compressNode(node ir.TextNode, request PipelineRequest, stats *telemetry.PipelineStats) string {
	result := CompressText(CompressRequest{
		Text:    node.Text,
		Level:   request.Level,
		Context: contextOf(node),
	})
	emptied := hasEmptied(node.Text, result.Text)
	text := result.Text
	if emptied {
		text = node.Text
	}
	stats.NodesSeen++
	stats.CharsBefore += result.Stats.CharsIn
	stats.CharsAfter += len(text)
	stats.CharsProse += result.Stats.CharsProse
	if !emptied && !result.Stats.IsUncompressed {
		stats.NodesCompressed++
	}
	return text
}

// RunPipeline compresses every in-scope text node and reports what it cost. The
// walk runs whatever the level, so the stats describe the same nodes at every
// setting, which is what makes one level's result comparable to another's.
//
// Under the default CacheIgnore mode a breakpoint changes nothing. A cached
// prefix matches on its bytes being identical from one turn to the next.
// Compression is deterministic and reads nothing positional, so a node compressed
// on the turn it first appears re-renders identically for the rest of the session
// and the prefix settles in compressed form. The turn it first compresses costs
// one write of the segment it lies in, which a growing conversation was going to
// write anyway.
func RunPipeline(request PipelineRequest) PipelineResult {
	stats := telemetry.PipelineStats{Level: request.Level}
	cachedThrough := cachedThroughIndex(request)
	index := -1
	compressed := ir.MapTextNodes(request.Request, request.Scopes, func(node ir.TextNode) string {
		index++
		if index <= cachedThrough {
			return skipNode(node, &stats)
		}
		return compressNode(node, request, &stats)
	})
	return PipelineResult{Request: compressed, Stats: stats}
}
