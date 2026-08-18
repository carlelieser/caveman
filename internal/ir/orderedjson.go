package ir

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value is one decoded JSON value. Objects keep their keys in the order they
// arrived: prompt cache lookup matches on the serialized request prefix, so a
// body rebuilt with sorted keys carries identical content and still misses the
// cache. encoding/json's map[string]any loses that order, so it is never used
// for anything that returns to the wire.
type Value interface {
	writeTo(dst *strings.Builder)
}

type (
	Null   struct{}
	Bool   bool
	String string
	Array  []Value
)

// Number keeps the literal bytes it was parsed from. Re-formatting a float
// through strconv can widen 0.7 or narrow 1e21, and either changes the bytes.
type Number struct {
	literal string
}

// Member is one key/value pair of an Object.
type Member struct {
	Key   string
	Value Value
}

// Object is a JSON object in wire order. Duplicate keys are not represented:
// the parser keeps the last value at the position of the first key, which is
// what JavaScript's own object construction does.
type Object struct {
	members []Member
	index   map[string]int
}

func NewObject() *Object {
	return &Object{index: map[string]int{}}
}

func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.members)
}

func (o *Object) Members() []Member {
	if o == nil {
		return nil
	}
	return o.members
}

func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	keys := make([]string, len(o.members))
	for i, member := range o.members {
		keys[i] = member.Key
	}
	return keys
}

// Get reports presence separately from the value, because `'k' in obj` and
// `obj.k !== undefined` are different questions and the adapter asks both.
func (o *Object) Get(key string) (Value, bool) {
	if o == nil {
		return nil, false
	}
	at, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.members[at].Value, true
}

func (o *Object) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

func (o *Object) Set(key string, value Value) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if at, ok := o.index[key]; ok {
		o.members[at].Value = value
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, Member{Key: key, Value: value})
}

func (o *Object) Delete(key string) {
	at, ok := o.index[key]
	if !ok {
		return
	}
	o.members = append(o.members[:at], o.members[at+1:]...)
	delete(o.index, key)
	for i := at; i < len(o.members); i++ {
		o.index[o.members[i].Key] = i
	}
}

// Clone copies the spine so a rebuilt body never shares an Object with the one
// it was derived from. Scalars are immutable and are shared.
func (o *Object) Clone() *Object {
	if o == nil {
		return nil
	}
	out := &Object{
		members: make([]Member, len(o.members)),
		index:   make(map[string]int, len(o.index)),
	}
	for i, member := range o.members {
		out.members[i] = Member{Key: member.Key, Value: CloneValue(member.Value)}
		out.index[member.Key] = i
	}
	return out
}

func CloneValue(value Value) Value {
	switch typed := value.(type) {
	case *Object:
		return typed.Clone()
	case Array:
		out := make(Array, len(typed))
		for i, item := range typed {
			out[i] = CloneValue(item)
		}
		return out
	default:
		return value
	}
}

func NumberFromLiteral(literal string) Number {
	return Number{literal: literal}
}

func NumberFromInt(n int) Number {
	return Number{literal: strconv.Itoa(n)}
}

// NumberFromFloat formats the way JavaScript's Number-to-string does, so a
// value the IR computed rather than copied serializes as the client would have
// written it.
func NumberFromFloat(f float64) Number {
	return Number{literal: formatJSNumber(f)}
}

func (n Number) Literal() string { return n.literal }

func (n Number) Float() float64 {
	f, err := strconv.ParseFloat(n.literal, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

func (Null) writeTo(dst *strings.Builder) { dst.WriteString("null") }

func (b Bool) writeTo(dst *strings.Builder) {
	if b {
		dst.WriteString("true")
		return
	}
	dst.WriteString("false")
}

func (n Number) writeTo(dst *strings.Builder) {
	if n.literal == "" {
		dst.WriteString("0")
		return
	}
	dst.WriteString(n.literal)
}

func (s String) writeTo(dst *strings.Builder) { writeJSString(dst, string(s)) }

func (a Array) writeTo(dst *strings.Builder) {
	dst.WriteByte('[')
	for i, item := range a {
		if i > 0 {
			dst.WriteByte(',')
		}
		if item == nil {
			dst.WriteString("null")
			continue
		}
		item.writeTo(dst)
	}
	dst.WriteByte(']')
}

func (o *Object) writeTo(dst *strings.Builder) {
	dst.WriteByte('{')
	for i, member := range o.members {
		if i > 0 {
			dst.WriteByte(',')
		}
		writeJSString(dst, member.Key)
		dst.WriteByte(':')
		if member.Value == nil {
			dst.WriteString("null")
			continue
		}
		member.Value.writeTo(dst)
	}
	dst.WriteByte('}')
}

// Marshal produces the bytes JSON.stringify would. It differs from
// encoding/json in two ways that matter on the wire: `<`, `>` and `&` are not
// escaped, and object keys keep their insertion order.
func Marshal(value Value) []byte {
	var out strings.Builder
	if value == nil {
		return []byte("null")
	}
	value.writeTo(&out)
	return []byte(out.String())
}

func MarshalString(value Value) string {
	return string(Marshal(value))
}

var hexDigits = "0123456789abcdef"

func writeJSString(dst *strings.Builder, s string) {
	dst.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '"':
				dst.WriteString(`\"`)
			case '\\':
				dst.WriteString(`\\`)
			case '\b':
				dst.WriteString(`\b`)
			case '\f':
				dst.WriteString(`\f`)
			case '\n':
				dst.WriteString(`\n`)
			case '\r':
				dst.WriteString(`\r`)
			case '\t':
				dst.WriteString(`\t`)
			default:
				if c < 0x20 {
					dst.WriteString(`\u00`)
					dst.WriteByte(hexDigits[c>>4])
					dst.WriteByte(hexDigits[c&0xf])
				} else {
					dst.WriteByte(c)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst.WriteString(`�`)
			i++
			continue
		}
		dst.WriteString(s[i : i+size])
		i += size
	}
	dst.WriteByte('"')
}

// Unmarshal decodes JSON while keeping object key order and number literals.
func Unmarshal(data []byte) (Value, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing content after JSON value")
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder) (Value, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return decodeFromToken(decoder, token)
}

func decodeFromToken(decoder *json.Decoder, token json.Token) (Value, error) {
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			return decodeObject(decoder)
		case '[':
			return decodeArray(decoder)
		}
		return nil, fmt.Errorf("unexpected delimiter %q", typed)
	case string:
		return String(typed), nil
	case json.Number:
		return Number{literal: typed.String()}, nil
	case bool:
		return Bool(typed), nil
	case nil:
		return Null{}, nil
	}
	return nil, fmt.Errorf("unexpected token %T", token)
}

func decodeObject(decoder *json.Decoder) (Value, error) {
	object := NewObject()
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("object key was %T", keyToken)
		}
		value, err := decodeValue(decoder)
		if err != nil {
			return nil, err
		}
		object.Set(key, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return object, nil
}

func decodeArray(decoder *json.Decoder) (Value, error) {
	items := Array{}
	for decoder.More() {
		item, err := decodeValue(decoder)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return items, nil
}

// formatJSNumber reproduces JavaScript's Number-to-string algorithm: the
// shortest decimal that round-trips, with exponent form only outside the
// 1e-7 .. 1e21 window.
func formatJSNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	if f == 0 {
		return "0"
	}
	// ECMAScript picks the fixed form when the shortest representation's decimal
	// exponent falls in [-6, 21), and the exponent form otherwise. The decision
	// is made on that shortest form, not on the magnitude: 9.9e-7 prints as
	// 9.9e-7 while 0.000001 prints in full.
	shortest := strconv.FormatFloat(f, 'e', -1, 64)
	exponent, err := strconv.Atoi(shortest[strings.IndexByte(shortest, 'e')+1:])
	if err != nil {
		return shortest
	}
	if exponent >= -6 && exponent < 21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return trimExponentZeros(shortest)
}

// Go writes a zero-padded exponent (1e-07); JavaScript does not (1e-7).
func trimExponentZeros(formatted string) string {
	at := strings.IndexByte(formatted, 'e')
	if at < 0 {
		return formatted
	}
	mantissa, exponent := formatted[:at], formatted[at+1:]
	sign := ""
	if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
		sign, exponent = string(exponent[0]), exponent[1:]
	}
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}
	return mantissa + "e" + sign + exponent
}
