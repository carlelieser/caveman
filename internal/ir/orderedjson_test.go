package ir_test

import (
	"testing"

	"github.com/carlelieser/caveman/internal/ir"
)

func reserialize(t *testing.T, text string) string {
	t.Helper()
	value, err := ir.Unmarshal([]byte(text))
	if err != nil {
		t.Fatalf("parsing %s: %v", text, err)
	}
	return ir.MarshalString(value)
}

// Every one of these differs from what encoding/json would emit, which is why
// the codec exists at all.
func TestReserializationIsByteExact(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"key order is not sorted", `{"z":1,"a":2,"m":3}`},
		{"nested key order is kept", `{"outer":{"z":1,"a":2},"a":{"y":1,"b":2}}`},
		{"html characters are not escaped", `{"t":"a < b && c > d"}`},
		{"float literals keep their written form", `{"a":0.7,"b":0.95,"c":1.0,"d":1e3,"e":-0.0}`},
		{"large integers are not widened to floats", `{"n":10000000000000000000000}`},
		{"null is distinct from absent", `{"a":null}`},
		{"empty containers survive", `{"a":[],"b":{}}`},
		{"escapes are re-emitted the same way", `{"t":"line\nbreak\ttab\"quote\\slash"}`},
		{"control characters use the short escape form", `{"t":"\u0001\u001f"}`},
		{"non-ascii stays raw", `{"t":"héllo — 世界 🙂"}`},
		{"duplicate keys collapse to the last value", `{"a":1,"b":2,"a":3}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want := test.json
			if test.name == "duplicate keys collapse to the last value" {
				want = `{"a":3,"b":2}`
			}
			if got := reserialize(t, test.json); got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestObjectDistinguishesAbsentFromNull(t *testing.T) {
	value, err := ir.Unmarshal([]byte(`{"present":null}`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	object := value.(*ir.Object)

	got, ok := object.Get("present")
	if !ok {
		t.Error("a key holding null reported as absent")
	}
	if _, isNull := got.(ir.Null); !isNull {
		t.Errorf("expected Null, got %T", got)
	}
	if _, ok := object.Get("missing"); ok {
		t.Error("an absent key reported as present")
	}
}

func TestSetKeepsInsertionOrderAndOverwritesInPlace(t *testing.T) {
	object := ir.NewObject()
	object.Set("a", ir.Number(ir.NumberFromInt(1)))
	object.Set("b", ir.Number(ir.NumberFromInt(2)))
	object.Set("a", ir.Number(ir.NumberFromInt(3)))
	if got := ir.MarshalString(object); got != `{"a":3,"b":2}` {
		t.Errorf("got %s, want {\"a\":3,\"b\":2}", got)
	}
}

func TestDeleteReindexesRemainingKeys(t *testing.T) {
	object := ir.NewObject()
	for _, key := range []string{"a", "b", "c"} {
		object.Set(key, ir.String(key))
	}
	object.Delete("a")
	if got := ir.MarshalString(object); got != `{"b":"b","c":"c"}` {
		t.Fatalf("got %s", got)
	}
	if value, ok := object.Get("c"); !ok || value != ir.String("c") {
		t.Error("delete left the index pointing at the wrong member")
	}
}

func TestCloneDoesNotShareMutableState(t *testing.T) {
	source := ir.NewObject()
	nested := ir.NewObject()
	nested.Set("inner", ir.String("original"))
	source.Set("outer", nested)

	clone := source.Clone()
	cloned, _ := clone.Get("outer")
	cloned.(*ir.Object).Set("inner", ir.String("changed"))

	if got := ir.MarshalString(source); got != `{"outer":{"inner":"original"}}` {
		t.Errorf("mutating the clone reached the source: %s", got)
	}
}

func TestNumberFromFloatMatchesJavaScript(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{0.1, "0.1"},
		{1.5e21, "1.5e+21"},
		{1e-7, "1e-7"},
		{1.5e-7, "1.5e-7"},
		{9.9e-7, "9.9e-7"},
		{1e-6, "0.000001"},
		{1e-21, "1e-21"},
		{1e20, "100000000000000000000"},
		{1e21, "1e+21"},
		{-0.5, "-0.5"},
	}
	for _, test := range cases {
		if got := ir.NumberFromFloat(test.value).Literal(); got != test.want {
			t.Errorf("NumberFromFloat(%v) = %s, want %s", test.value, got, test.want)
		}
	}
}

func TestUnmarshalRejectsTrailingContent(t *testing.T) {
	if _, err := ir.Unmarshal([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Error("expected an error for trailing content")
	}
}
