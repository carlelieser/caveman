package anthropic

import (
	"math"
	"strconv"
	"strings"

	"github.com/carlelieser/caveman/internal/ir"
)

// thinking and redacted_thinking carry a signature the API validates, so they
// are kept as raw wire values rather than being decomposed.
var thinkingTypes = []string{"thinking", "redacted_thinking"}

// jsString reproduces JavaScript's `String(value ?? ”)`. Absent and null both
// reach the empty string; everything else takes the coercion the language
// would apply, so a body carrying the wrong type still round-trips as one.
func jsString(value ir.Value, present bool) string {
	if !present {
		return ""
	}
	switch typed := value.(type) {
	case nil, ir.Null:
		return ""
	case ir.String:
		return string(typed)
	case ir.Bool:
		if typed {
			return "true"
		}
		return "false"
	case ir.Number:
		return typed.Literal()
	case ir.Array:
		parts := make([]string, len(typed))
		for i, item := range typed {
			if _, isNull := item.(ir.Null); isNull || item == nil {
				parts[i] = ""
				continue
			}
			parts[i] = jsString(item, true)
		}
		return strings.Join(parts, ",")
	case *ir.Object:
		return "[object Object]"
	}
	return ""
}

// jsNumber reproduces JavaScript's `Number(value ?? 0)`, keeping the literal
// the wire carried when the value was already a number so re-serializing it
// cannot change the bytes.
func jsNumber(value ir.Value, present bool) ir.Number {
	if !present {
		return ir.NumberFromInt(0)
	}
	switch typed := value.(type) {
	case nil, ir.Null:
		return ir.NumberFromInt(0)
	case ir.Number:
		return typed
	case ir.Bool:
		if typed {
			return ir.NumberFromInt(1)
		}
		return ir.NumberFromInt(0)
	case ir.String:
		return numberFromString(string(typed))
	case ir.Array:
		if len(typed) == 0 {
			return ir.NumberFromInt(0)
		}
		if len(typed) == 1 {
			return jsNumber(typed[0], true)
		}
	}
	return ir.NumberFromFloat(math.NaN())
}

func numberFromString(text string) ir.Number {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ir.NumberFromInt(0)
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return ir.NumberFromFloat(math.NaN())
	}
	return ir.NumberFromFloat(parsed)
}

func toOpaque(raw ir.Value) ir.Content {
	return &ir.OpaqueContent{Raw: raw}
}

func toTextContent(block *ir.Object) ir.Content {
	text, present := block.Get("text")
	content := &ir.TextContent{
		Text:     jsString(text, present),
		KeyOrder: ir.KeyOrder(block.Keys()),
	}
	if cacheControl, ok := block.Get("cache_control"); ok {
		content.CacheControl = cacheControl
	}
	content.Passthrough = extractPassthrough(block, modelledTextKeys)
	return content
}

func toToolUseContent(block *ir.Object) ir.Content {
	id, hasID := block.Get("id")
	name, hasName := block.Get("name")
	input, hasInput := block.Get("input")
	content := &ir.ToolUseContent{
		ID:       jsString(id, hasID),
		Name:     jsString(name, hasName),
		Input:    input,
		HasInput: hasInput,
		KeyOrder: ir.KeyOrder(block.Keys()),
	}
	if cacheControl, ok := block.Get("cache_control"); ok {
		content.CacheControl = cacheControl
	}
	content.Passthrough = extractPassthrough(block, modelledToolUseKeys)
	return content
}

func toToolResultContent(block *ir.Object) ir.Content {
	raw, _ := block.Get("content")
	_, isContentString := asString(raw)
	toolUseID, hasToolUseID := block.Get("tool_use_id")
	content := &ir.ToolResultContent{
		ToolUseID:       jsString(toolUseID, hasToolUseID),
		Content:         toContentArray(raw),
		IsContentString: isContentString,
		KeyOrder:        ir.KeyOrder(block.Keys()),
	}
	if isError, ok := block.Get("is_error"); ok {
		flag := isError == ir.Bool(true)
		content.IsError = &flag
	}
	if cacheControl, ok := block.Get("cache_control"); ok {
		content.CacheControl = cacheControl
	}
	content.Passthrough = extractPassthrough(block, modelledToolResultKeys)
	return content
}

func toContentBlock(block ir.Value) ir.Content {
	object, ok := asObject(block)
	if !ok {
		return toOpaque(block)
	}
	rawType, _ := object.Get("type")
	blockType, isString := asString(rawType)
	if !isString {
		return toOpaque(block)
	}
	switch blockType {
	case "text":
		return toTextContent(object)
	case "tool_use":
		return toToolUseContent(object)
	case "tool_result":
		return toToolResultContent(object)
	}
	if contains(thinkingTypes, blockType) {
		return &ir.ThinkingContent{Raw: block}
	}
	return toOpaque(block)
}

// toContentArray normalizes the string-or-array content forms to an array. The
// original form is recorded separately so FromIR can reproduce it.
func toContentArray(raw ir.Value) []ir.Content {
	if raw == nil {
		return []ir.Content{}
	}
	if _, isNull := raw.(ir.Null); isNull {
		return []ir.Content{}
	}
	if text, ok := asString(raw); ok {
		return []ir.Content{&ir.TextContent{Text: text}}
	}
	items, ok := asArray(raw)
	if !ok {
		return []ir.Content{toOpaque(raw)}
	}
	blocks := make([]ir.Content, len(items))
	for i, item := range items {
		blocks[i] = toContentBlock(item)
	}
	return blocks
}

func toMessage(raw ir.Value) ir.Message {
	object, ok := asObject(raw)
	if !ok {
		return ir.Message{Role: ir.RoleUser, Content: []ir.Content{toOpaque(raw)}}
	}
	content, _ := object.Get("content")
	_, isContentString := asString(content)
	role, _ := object.Get("role")
	roleText, roleIsString := asString(role)
	message := ir.Message{
		Role:            ir.Role(roleText),
		Content:         toContentArray(content),
		IsContentString: isContentString,
		KeyOrder:        ir.KeyOrder(object.Keys()),
	}
	// The IR types role as a string. A wire body carrying anything else keeps
	// its value in passthrough rather than losing it to the coercion.
	modelled := modelledMessageKeys
	if !roleIsString {
		modelled = []string{"content"}
	}
	message.Passthrough = extractPassthrough(object, modelled)
	return message
}

// Only a string or block-array `system` is modelled; any other form (including
// an explicit null) stays in passthrough so it round-trips as written.
func isSystemModelled(body *ir.Object) bool {
	system, _ := body.Get("system")
	if _, ok := asString(system); ok {
		return true
	}
	_, ok := asArray(system)
	return ok
}

func toSystem(raw ir.Value) (system []ir.Content, hasSystem bool, isSystemString bool) {
	if _, ok := asString(raw); !ok {
		if _, isArray := asArray(raw); !isArray {
			return nil, false, false
		}
	}
	_, isString := asString(raw)
	return toContentArray(raw), true, isString
}

func toTools(raw ir.Value) []ir.Tool {
	items, ok := asArray(raw)
	if !ok {
		return []ir.Tool{}
	}
	tools := make([]ir.Tool, len(items))
	for i, item := range items {
		tools[i] = ir.Tool{Raw: item}
	}
	return tools
}

// An empty `tools: []` is indistinguishable from an absent one once modelled as
// a slice, so it stays in passthrough and only a populated list is modelled.
func isToolsModelled(body *ir.Object) bool {
	tools, _ := body.Get("tools")
	items, ok := asArray(tools)
	return ok && len(items) > 0
}

func requestKeysToModel(body *ir.Object) []string {
	kept := make([]string, 0, len(modelledRequestKeys))
	for _, key := range modelledRequestKeys {
		if key == "tools" && !isToolsModelled(body) {
			continue
		}
		if key == "system" && !isSystemModelled(body) {
			continue
		}
		kept = append(kept, key)
	}
	return kept
}

// ToIR converts an Anthropic request body into the IR. Unknown fields and
// unknown block types are preserved rather than dropped, so an unrecognized
// future shape degrades to passthrough instead of data loss.
func ToIR(body *ir.Object) ir.Request {
	rawSystem, _ := body.Get("system")
	system, hasSystem, isSystemString := toSystem(rawSystem)

	rawMessages, _ := body.Get("messages")
	messageItems, _ := asArray(rawMessages)
	messages := make([]ir.Message, len(messageItems))
	for i, item := range messageItems {
		messages[i] = toMessage(item)
	}

	model, hasModel := body.Get("model")
	maxTokens, hasMaxTokens := body.Get("max_tokens")

	tools := []ir.Tool{}
	if isToolsModelled(body) {
		rawTools, _ := body.Get("tools")
		tools = toTools(rawTools)
	}

	passthrough := extractPassthrough(body, requestKeysToModel(body))
	if passthrough == nil {
		passthrough = ir.NewObject()
	}

	return ir.Request{
		Model:     jsString(model, hasModel),
		MaxTokens: jsNumber(maxTokens, hasMaxTokens),
		System:    system,
		HasSystem: hasSystem,
		Messages:  messages,
		Tools:     tools,
		Extensions: ir.Extensions{
			IsSystemString: isSystemString,
			KeyOrder:       ir.KeyOrder(body.Keys()),
		},
		Passthrough: passthrough,
	}
}
