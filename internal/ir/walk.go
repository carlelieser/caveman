package ir

type Scope string

const (
	ScopeMessages    Scope = "messages"
	ScopeSystem      Scope = "system"
	ScopeToolResults Scope = "tool_results"
)

var AllScopes = []Scope{ScopeMessages, ScopeSystem, ScopeToolResults}

// TextNodePath is the address of a text node within a Request, stable for the
// duration of one walk. MessageIndex is -1 for system text and ToolResultIndex
// is -1 for text that is not nested inside a tool_result.
type TextNodePath struct {
	Scope           Scope
	MessageIndex    int
	BlockIndex      int
	ToolResultIndex int
}

const NoIndex = -1

// TextNode is a compressible text node plus the context compression needs to
// weigh it. Role is empty for a node with no owning role.
type TextNode struct {
	Text            string
	Role            Role
	Path            TextNodePath
	HasCacheControl bool
}

type TextVisitor func(node TextNode)

type TextMapper func(node TextNode) string

type scopeSet map[Scope]bool

func toScopeSet(scopes []Scope) scopeSet {
	set := make(scopeSet, len(scopes))
	for _, scope := range scopes {
		set[scope] = true
	}
	return set
}

func systemPath(blockIndex int) TextNodePath {
	return TextNodePath{Scope: ScopeSystem, MessageIndex: NoIndex, BlockIndex: blockIndex, ToolResultIndex: NoIndex}
}

func messagePath(messageIndex, blockIndex int) TextNodePath {
	return TextNodePath{Scope: ScopeMessages, MessageIndex: messageIndex, BlockIndex: blockIndex, ToolResultIndex: NoIndex}
}

func toolResultPath(messageIndex, blockIndex, toolResultIndex int) TextNodePath {
	return TextNodePath{Scope: ScopeToolResults, MessageIndex: messageIndex, BlockIndex: blockIndex, ToolResultIndex: toolResultIndex}
}

func toTextNode(block *TextContent, role Role, path TextNodePath) TextNode {
	return TextNode{
		Text:            block.Text,
		Role:            role,
		Path:            path,
		HasCacheControl: block.CacheControl != nil,
	}
}

func mapTextBlock(block *TextContent, node TextNode, mapper TextMapper) Content {
	text := mapper(node)
	if text == block.Text {
		return block
	}
	replaced := *block
	replaced.Text = text
	return &replaced
}

func mapMessageBlocks(message Message, messageIndex int, scopes scopeSet, mapper TextMapper) []Content {
	out := make([]Content, len(message.Content))
	for blockIndex, block := range message.Content {
		out[blockIndex] = block
		if text, ok := block.(*TextContent); ok {
			if scopes[ScopeMessages] {
				path := messagePath(messageIndex, blockIndex)
				out[blockIndex] = mapTextBlock(text, toTextNode(text, message.Role, path), mapper)
			}
			continue
		}
		result, ok := block.(*ToolResultContent)
		if !ok || !scopes[ScopeToolResults] {
			continue
		}
		nested := make([]Content, len(result.Content))
		changed := false
		for nestedIndex, inner := range result.Content {
			nested[nestedIndex] = inner
			text, ok := inner.(*TextContent)
			if !ok {
				continue
			}
			path := toolResultPath(messageIndex, blockIndex, nestedIndex)
			nested[nestedIndex] = mapTextBlock(text, toTextNode(text, message.Role, path), mapper)
			if nested[nestedIndex] != inner {
				changed = true
			}
		}
		if !changed {
			continue
		}
		replaced := *result
		replaced.Content = nested
		out[blockIndex] = &replaced
	}
	return out
}

func mapSystem(system []Content, hasSystem bool, scopes scopeSet, mapper TextMapper) []Content {
	if !hasSystem || !scopes[ScopeSystem] {
		return system
	}
	out := make([]Content, len(system))
	for blockIndex, block := range system {
		out[blockIndex] = block
		text, ok := block.(*TextContent)
		if !ok {
			continue
		}
		out[blockIndex] = mapTextBlock(text, toTextNode(text, RoleSystem, systemPath(blockIndex)), mapper)
	}
	return out
}

// MapTextNodes returns a new Request with every in-scope text node replaced by
// the mapper's result. The input is never mutated; untouched nodes keep their
// original pointer identity.
func MapTextNodes(request Request, scopes []Scope, mapper TextMapper) Request {
	scopeSet := toScopeSet(scopes)
	// System is mapped first so a visitor observes nodes in document order.
	system := mapSystem(request.System, request.HasSystem, scopeSet, mapper)
	messages := make([]Message, len(request.Messages))
	for messageIndex, message := range request.Messages {
		messages[messageIndex] = message
		messages[messageIndex].Content = mapMessageBlocks(message, messageIndex, scopeSet, mapper)
	}
	out := request
	out.System = system
	out.Messages = messages
	return out
}

// ForEachTextNode visits every in-scope text node in document order without
// modifying anything.
func ForEachTextNode(request Request, scopes []Scope, visitor TextVisitor) {
	MapTextNodes(request, scopes, func(node TextNode) string {
		visitor(node)
		return node.Text
	})
}

// CollectTextNodes collects every in-scope text node in document order.
func CollectTextNodes(request Request, scopes []Scope) []TextNode {
	nodes := []TextNode{}
	ForEachTextNode(request, scopes, func(node TextNode) {
		nodes = append(nodes, node)
	})
	return nodes
}
