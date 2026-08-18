package anthropic

import "github.com/carlelieser/caveman/internal/ir"

func fromTextContent(block *ir.TextContent) ir.Value {
	wire := ir.NewObject()
	wire.Set("type", ir.String("text"))
	wire.Set("text", ir.String(block.Text))
	setIfPresent(wire, "cache_control", block.CacheControl)
	return inKeyOrder(applyPassthrough(wire, block.Passthrough), block.KeyOrder)
}

func fromToolUseContent(block *ir.ToolUseContent) ir.Value {
	wire := ir.NewObject()
	wire.Set("type", ir.String("tool_use"))
	wire.Set("id", ir.String(block.ID))
	wire.Set("name", ir.String(block.Name))
	if block.HasInput {
		wire.Set("input", block.Input)
	}
	setIfPresent(wire, "cache_control", block.CacheControl)
	return inKeyOrder(applyPassthrough(wire, block.Passthrough), block.KeyOrder)
}

func fromToolResultContent(block *ir.ToolResultContent) ir.Value {
	wire := ir.NewObject()
	wire.Set("type", ir.String("tool_result"))
	wire.Set("tool_use_id", ir.String(block.ToolUseID))
	setIfPresent(wire, "content", fromContentField(block.Content, block.IsContentString))
	if block.IsError != nil {
		wire.Set("is_error", ir.Bool(*block.IsError))
	}
	setIfPresent(wire, "cache_control", block.CacheControl)
	return inKeyOrder(applyPassthrough(wire, block.Passthrough), block.KeyOrder)
}

func fromContentBlock(block ir.Content) ir.Value {
	switch typed := block.(type) {
	case *ir.TextContent:
		return fromTextContent(typed)
	case *ir.ToolUseContent:
		return fromToolUseContent(typed)
	case *ir.ToolResultContent:
		return fromToolResultContent(typed)
	case *ir.ThinkingContent:
		return typed.Raw
	case *ir.OpaqueContent:
		return typed.Raw
	}
	return ir.Null{}
}

func fromContentBlocks(content []ir.Content) ir.Array {
	items := make(ir.Array, len(content))
	for i, block := range content {
		items[i] = fromContentBlock(block)
	}
	return items
}

// fromContentField reproduces the string-or-array form the content arrived in.
// A string form is only recoverable from a single text block, which is what
// ToIR produced. A nil result means the field is absent.
func fromContentField(content []ir.Content, isContentString bool) ir.Value {
	if !isContentString {
		if len(content) == 0 {
			return nil
		}
		return fromContentBlocks(content)
	}
	if len(content) > 0 {
		if text, ok := content[0].(*ir.TextContent); ok {
			return ir.String(text.Text)
		}
	}
	return fromContentBlocks(content)
}

func fromMessage(message ir.Message) ir.Value {
	wire := ir.NewObject()
	// An empty role is the absent one: the IR types role as a string, so a
	// message that arrived without one must not gain an empty one here.
	if message.Role != "" {
		wire.Set("role", ir.String(message.Role))
	}
	content := fromContentField(message.Content, message.IsContentString)
	if content == nil {
		content = ir.Array{}
	}
	wire.Set("content", content)
	return inKeyOrder(applyPassthrough(wire, message.Passthrough), message.KeyOrder)
}

func fromSystem(request ir.Request) ir.Value {
	if !request.HasSystem {
		return nil
	}
	if request.Extensions.IsSystemString && len(request.System) > 0 {
		if text, ok := request.System[0].(*ir.TextContent); ok {
			return ir.String(text.Text)
		}
	}
	return fromContentBlocks(request.System)
}

// FromIR converts the IR back to an Anthropic request body. Passthrough fields
// are restored last so a modelled key never shadows the value that arrived.
func FromIR(request ir.Request) *ir.Object {
	body := ir.NewObject()
	body.Set("model", ir.String(request.Model))
	body.Set("max_tokens", request.MaxTokens)
	setIfPresent(body, "system", fromSystem(request))

	messages := make(ir.Array, len(request.Messages))
	for i, message := range request.Messages {
		messages[i] = fromMessage(message)
	}
	body.Set("messages", messages)

	if len(request.Tools) > 0 {
		tools := make(ir.Array, len(request.Tools))
		for i, tool := range request.Tools {
			tools[i] = tool.Raw
		}
		body.Set("tools", tools)
	}

	return inKeyOrder(applyPassthrough(body, request.Passthrough), request.Extensions.KeyOrder)
}
