package anthropic

import "github.com/carlelieser/caveman/internal/ir"

// Top-level request keys the IR models explicitly. Everything else is passthrough.
var modelledRequestKeys = []string{"model", "max_tokens", "system", "messages", "tools"}

var modelledMessageKeys = []string{"role", "content"}

var modelledTextKeys = []string{"type", "text", "cache_control"}

var modelledToolUseKeys = []string{"type", "id", "name", "input", "cache_control"}

var modelledToolResultKeys = []string{"type", "tool_use_id", "content", "is_error", "cache_control"}

func contains(keys []string, key string) bool {
	for _, candidate := range keys {
		if candidate == key {
			return true
		}
	}
	return false
}

// extractPassthrough returns every key of source not named in modelled, or nil
// when there are none, so an absent passthrough never materializes as an empty
// object on the way back out.
func extractPassthrough(source *ir.Object, modelled []string) *ir.Object {
	rest := ir.NewObject()
	for _, member := range source.Members() {
		if contains(modelled, member.Key) {
			continue
		}
		rest.Set(member.Key, member.Value)
	}
	if rest.Len() == 0 {
		return nil
	}
	return rest
}

// applyPassthrough copies passthrough keys back onto a rebuilt wire object.
func applyPassthrough(target *ir.Object, passthrough *ir.Object) *ir.Object {
	for _, member := range passthrough.Members() {
		target.Set(member.Key, member.Value)
	}
	return target
}

// inKeyOrder re-emits a rebuilt wire object in the key order it arrived in.
// JSON key order is insertion order, and prompt cache lookup matches on
// serialized bytes, so a body reassembled in declaration order misses the cache
// despite being equal.
//
// Keys recorded but no longer present are skipped — a field the IR dropped must
// not reappear. Keys present but never recorded are appended, so a synthesized
// field still survives.
func inKeyOrder(built *ir.Object, keyOrder ir.KeyOrder) *ir.Object {
	if keyOrder == nil {
		return built
	}
	ordered := ir.NewObject()
	for _, key := range keyOrder {
		if value, ok := built.Get(key); ok {
			ordered.Set(key, value)
		}
	}
	for _, member := range built.Members() {
		if !ordered.Has(member.Key) {
			ordered.Set(member.Key, member.Value)
		}
	}
	return ordered
}

// setIfPresent assigns value under key only when it is present, so an optional
// field absent on the way in never reappears on the wire. A JSON null is
// present; only a nil Value is absent.
func setIfPresent(target *ir.Object, key string, value ir.Value) {
	if value == nil {
		return
	}
	target.Set(key, value)
}

func asObject(value ir.Value) (*ir.Object, bool) {
	object, ok := value.(*ir.Object)
	return object, ok
}

func asString(value ir.Value) (string, bool) {
	text, ok := value.(ir.String)
	return string(text), ok
}

func asArray(value ir.Value) (ir.Array, bool) {
	items, ok := value.(ir.Array)
	return items, ok
}
