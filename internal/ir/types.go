package ir

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type Kind string

const (
	KindText       Kind = "text"
	KindToolResult Kind = "tool_result"
	KindToolUse    Kind = "tool_use"
	KindThinking   Kind = "thinking"
	KindOpaque     Kind = "opaque"
)

// KeyOrder is the key order of one wire object, recorded so it can be rebuilt
// byte-for-byte. Carried separately from the values because the IR models some
// keys as named fields and leaves the rest in passthrough.
type KeyOrder []string

func (k KeyOrder) Clone() KeyOrder {
	if k == nil {
		return nil
	}
	out := make(KeyOrder, len(k))
	copy(out, k)
	return out
}

// Content is one block of a message, system prompt, or tool result. The
// concrete types are the arms of a closed union; a type switch over them is
// exhaustive.
type Content interface {
	Kind() Kind
	cloneContent() Content
}

// TextContent is the only compressible block type.
type TextContent struct {
	Text string
	// CacheControl is nil when the block carried no cache_control. A present
	// JSON null is Null{}, which is a different thing and must survive as one.
	CacheControl Value
	// Passthrough holds wire fields the IR does not model, kept so the adapter
	// can restore the block exactly as it arrived.
	Passthrough *Object
	KeyOrder    KeyOrder
}

type ToolResultContent struct {
	ToolUseID string
	Content   []Content
	// IsContentString records that the block carried `content` as a bare string
	// rather than a block array.
	IsContentString bool
	// IsError is nil when the wire block had no is_error at all.
	IsError      *bool
	CacheControl Value
	Passthrough  *Object
	KeyOrder     KeyOrder
}

type ToolUseContent struct {
	ID           string
	Name         string
	Input        Value
	HasInput     bool
	CacheControl Value
	Passthrough  *Object
	KeyOrder     KeyOrder
}

// ThinkingContent carries a signature the API validates, so the block is kept
// as a raw wire value rather than being decomposed.
type ThinkingContent struct {
	Raw Value
}

// OpaqueContent is any block the adapter did not recognize. Kept verbatim so an
// unknown future shape degrades to passthrough instead of data loss.
type OpaqueContent struct {
	Raw Value
}

func (*TextContent) Kind() Kind       { return KindText }
func (*ToolResultContent) Kind() Kind { return KindToolResult }
func (*ToolUseContent) Kind() Kind    { return KindToolUse }
func (*ThinkingContent) Kind() Kind   { return KindThinking }
func (*OpaqueContent) Kind() Kind     { return KindOpaque }

func (c *TextContent) cloneContent() Content {
	out := *c
	out.CacheControl = CloneValue(c.CacheControl)
	out.Passthrough = c.Passthrough.Clone()
	out.KeyOrder = c.KeyOrder.Clone()
	return &out
}

func (c *ToolResultContent) cloneContent() Content {
	out := *c
	out.Content = CloneContents(c.Content)
	if c.IsError != nil {
		flag := *c.IsError
		out.IsError = &flag
	}
	out.CacheControl = CloneValue(c.CacheControl)
	out.Passthrough = c.Passthrough.Clone()
	out.KeyOrder = c.KeyOrder.Clone()
	return &out
}

func (c *ToolUseContent) cloneContent() Content {
	out := *c
	out.Input = CloneValue(c.Input)
	out.CacheControl = CloneValue(c.CacheControl)
	out.Passthrough = c.Passthrough.Clone()
	out.KeyOrder = c.KeyOrder.Clone()
	return &out
}

func (c *ThinkingContent) cloneContent() Content {
	return &ThinkingContent{Raw: CloneValue(c.Raw)}
}

func (c *OpaqueContent) cloneContent() Content {
	return &OpaqueContent{Raw: CloneValue(c.Raw)}
}

func CloneContent(content Content) Content {
	if content == nil {
		return nil
	}
	return content.cloneContent()
}

func CloneContents(contents []Content) []Content {
	if contents == nil {
		return nil
	}
	out := make([]Content, len(contents))
	for i, item := range contents {
		out[i] = CloneContent(item)
	}
	return out
}

type Message struct {
	Role    Role
	Content []Content
	// IsContentString records that the message carried `content` as a bare
	// string rather than a block array. A provider without that duality leaves
	// it false, and the block-array form is what gets emitted.
	IsContentString bool
	Passthrough     *Object
	KeyOrder        KeyOrder
}

func (m Message) Clone() Message {
	out := m
	out.Content = CloneContents(m.Content)
	out.Passthrough = m.Passthrough.Clone()
	out.KeyOrder = m.KeyOrder.Clone()
	return out
}

// Tool definitions are never inspected or mutated; they round-trip verbatim.
type Tool struct {
	Raw Value
}

// Extensions is the provider-specific shape the IR must remember to reproduce
// the wire format, plus modelled provider features. Not compressible, never
// inspected by the pipeline.
type Extensions struct {
	// IsSystemString records that `system` arrived as a bare string rather than
	// an array of text blocks.
	IsSystemString bool
	// KeyOrder is the top-level key order as it arrived. Prompt cache lookup
	// matches on the serialized prefix, so a body rebuilt in a different order
	// misses the cache even though it carries identical content.
	KeyOrder KeyOrder
}

type Request struct {
	Model     string
	MaxTokens Number
	// System is nil when the wire body had no modelled system field. An empty
	// non-nil slice is a `system: []` that arrived.
	System      []Content
	HasSystem   bool
	Messages    []Message
	Tools       []Tool
	Extensions  Extensions
	Passthrough *Object
}

func (r Request) Clone() Request {
	out := r
	out.System = CloneContents(r.System)
	out.Messages = make([]Message, len(r.Messages))
	for i, message := range r.Messages {
		out.Messages[i] = message.Clone()
	}
	out.Tools = make([]Tool, len(r.Tools))
	for i, tool := range r.Tools {
		out.Tools[i] = Tool{Raw: CloneValue(tool.Raw)}
	}
	out.Extensions.KeyOrder = r.Extensions.KeyOrder.Clone()
	out.Passthrough = r.Passthrough.Clone()
	return out
}
