package ucl

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// upperString exercises the encoding.TextMarshaler and TextUnmarshaler paths
// in both directions.
type upperString string

var errBoom = errors.New("boom")

func (u *upperString) UnmarshalText(b []byte) error {
	*u = upperString(strings.ToUpper(string(b)))
	return nil
}

func (u upperString) MarshalText() ([]byte, error) {
	if u == "boom" {
		return nil, errBoom
	}
	return []byte(strings.ToLower(string(u))), nil
}

// Every conversion promised by Decode's documentation.
func TestDecodeConversions(t *testing.T) {
	var v struct {
		Ptr    *int           `ucl:"ptr"`
		Gone   *int           `ucl:"gone"`
		Text   upperString    `ucl:"text"`
		U8     uint8          `ucl:"u8"`
		U64    uint64         `ucl:"u64"`
		UStr   uint16         `ucl:"ustr"`
		FStr   float64        `ucl:"fstr"`
		F32    float32        `ucl:"f32"`
		BStr   bool           `ucl:"bstr"`
		BOff   bool           `ucl:"boff"`
		Bytes  []byte         `ucl:"bytes"`
		Counts map[string]int `ucl:"counts"`
		Fixed  [3]int         `ucl:"fixed"`
		Any    any            `ucl:"any"`
		NilArr []string       `ucl:"nilarr"`
	}
	v.Gone = new(int)
	*v.Gone = 7

	src := `ptr = 5
		gone = null
		text = quiet
		u8 = 200
		u64 = 9007199254740992
		ustr = "4096"
		fstr = "1.5"
		f32 = 0.5
		bstr = "yes"
		boff = off
		bytes = "raw"
		counts { a = 1, b = 2 }
		fixed = [1, 2, 3]
		any = [1, two]
		nilarr = null`
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}

	if v.Ptr == nil || *v.Ptr != 5 {
		t.Errorf("ptr is %v", v.Ptr)
	}
	if v.Gone != nil {
		t.Errorf("null should clear a pointer, got %v", *v.Gone)
	}
	if v.Text != "QUIET" {
		t.Errorf("text is %q", v.Text)
	}
	if v.U8 != 200 || v.U64 != 9007199254740992 || v.UStr != 4096 {
		t.Errorf("unsigned fields are %d %d %d", v.U8, v.U64, v.UStr)
	}
	if v.FStr != 1.5 || v.F32 != 0.5 {
		t.Errorf("float fields are %v %v", v.FStr, v.F32)
	}
	if !v.BStr || v.BOff {
		t.Errorf("bool fields are %v %v", v.BStr, v.BOff)
	}
	if string(v.Bytes) != "raw" {
		t.Errorf("bytes is %q", v.Bytes)
	}
	if !reflect.DeepEqual(v.Counts, map[string]int{"a": 1, "b": 2}) {
		t.Errorf("counts is %v", v.Counts)
	}
	if v.Fixed != [3]int{1, 2, 3} {
		t.Errorf("fixed is %v", v.Fixed)
	}
	if !reflect.DeepEqual(v.Any, []any{int64(1), "two"}) {
		t.Errorf("any is %#v", v.Any)
	}
	if v.NilArr != nil {
		t.Errorf("null should clear a slice, got %v", v.NilArr)
	}
}

func TestDecodeConversionErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		into any
		want string
	}{
		{"negative into uint", "a = -1", &struct {
			A uint `ucl:"a"`
		}{}, "does not fit"},
		{"overflow", "a = 300", &struct {
			A int8 `ucl:"a"`
		}{}, "overflows"},
		{"string into int", `a = "not a number"`, &struct {
			A int `ucl:"a"`
		}{}, "cannot use"},
		{"string into float", `a = "not a number"`, &struct {
			A float64 `ucl:"a"`
		}{}, "cannot use"},
		{"object into float", "a { b = 1 }", &struct {
			A float64 `ucl:"a"`
		}{}, "cannot use"},
		{"bad bool", `a = "maybe"`, &struct {
			A bool `ucl:"a"`
		}{}, "cannot use"},
		{"object into bool", "a { b = 1 }", &struct {
			A bool `ucl:"a"`
		}{}, "cannot use"},
		{"object into string", "a { b = 1 }", &struct {
			A string `ucl:"a"`
		}{}, "cannot use"},
		{"array too long", "a = [1, 2, 3]", &struct {
			A [2]int `ucl:"a"`
		}{}, "do not fit"},
		{"scalar into map", "a = 1", &struct {
			A map[string]int `ucl:"a"`
		}{}, "cannot use"},
		{"non string map key", "a { b = 1 }", &struct {
			A map[int]string `ucl:"a"`
		}{}, "unsupported map key"},
		{"non empty interface", "a = 1", &struct {
			A interface{ Foo() } `ucl:"a"`
		}{}, "cannot decode into"},
		{"bad duration", `a = "not a duration"`, &struct {
			A time.Duration `ucl:"a"`
		}{}, "cannot use"},
		{"object into duration", "a { b = 1 }", &struct {
			A time.Duration `ucl:"a"`
		}{}, "cannot use"},
		{"unsupported kind", "a = 1", &struct {
			A chan int `ucl:"a"`
		}{}, "cannot decode into"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Unmarshal([]byte(tc.src), tc.into)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Every conversion promised by FromInterface's documentation.
func TestEncodeConversions(t *testing.T) {
	type inner struct {
		N uint32 `ucl:"n"`
	}
	v := struct {
		Text   upperString         `ucl:"text"`
		Raw    *Object             `ucl:"raw"`
		Nested inner               `ucl:"nested"`
		Bytes  []byte              `ucl:"bytes"`
		Fixed  [3]int              `ucl:"fixed"`
		ByInt  map[int]string      `ucl:"byint"`
		ByText map[upperString]int `ucl:"bytext"`
		Ptr    *int                `ucl:"ptr"`
	}{
		Text:   "LOUD",
		Nested: inner{N: 7},
		Bytes:  []byte("raw"),
		Fixed:  [3]int{1, 2, 3},
		ByInt:  map[int]string{2: "b", 1: "a"},
		ByText: map[upperString]int{"B": 2, "A": 1},
	}

	obj, err := FromInterface(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"loud","raw":null,"nested":{"n":7},"bytes":"raw",` +
		`"fixed":[1,2,3],"byint":{"1":"a","2":"b"},"bytext":{"a":1,"b":2},"ptr":null}`
	if got := mustJSON(t, obj); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if obj.Get("bytes").Flags()&FlagBinary == 0 {
		t.Error("a []byte should carry the binary flag")
	}
}

func TestEncodeErrors(t *testing.T) {
	if _, err := FromInterface(struct {
		C chan int `ucl:"c"`
	}{}); err == nil {
		t.Error("a channel should not be encodable")
	}
	if _, err := FromInterface(struct {
		T upperString `ucl:"t"`
	}{T: "boom"}); err == nil {
		t.Error("a failing TextMarshaler should report its error")
	}
	if _, err := FromInterface(map[[2]int]string{{1, 2}: "x"}); err == nil {
		t.Error("an unsupported map key type should be reported")
	}
	if _, err := Marshal(struct {
		C chan int `ucl:"c"`
	}{}, JSON); err == nil {
		t.Error("Marshal should pass the conversion error through")
	}
}

// omitempty has to recognise the zero value of every kind.
func TestEncodeOmitEmpty(t *testing.T) {
	v := struct {
		B   bool           `ucl:"b,omitempty"`
		I   int            `ucl:"i,omitempty"`
		U   uint           `ucl:"u,omitempty"`
		F   float64        `ucl:"f,omitempty"`
		S   string         `ucl:"s,omitempty"`
		Sl  []int          `ucl:"sl,omitempty"`
		M   map[string]int `ucl:"m,omitempty"`
		P   *int           `ucl:"p,omitempty"`
		Any any            `ucl:"any,omitempty"`
		Arr [0]int         `ucl:"arr,omitempty"`
		Set int            `ucl:"set,omitempty"`
	}{Set: 1}

	obj, err := FromInterface(v)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, obj); got != `{"set":1}` {
		t.Errorf("got %s, want only the field that was set", got)
	}
}

func TestUnmarshalFile(t *testing.T) {
	var cfg struct {
		ABI   string   `ucl:"ABI"`
		Repos []string `ucl:"REPOS_DIR"`
	}
	if err := UnmarshalFile("../../testdata/pkg.conf", &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ABI != "FreeBSD:14:amd64" || len(cfg.Repos) != 2 {
		t.Errorf("got %+v", cfg)
	}
	if err := UnmarshalFile("../../testdata/does-not-exist.conf", &cfg); err == nil {
		t.Error("expected an error for a missing file")
	}
}

// MarshalJSON is what makes an *Object usable inside an encoding/json value.
func TestObjectMarshalJSON(t *testing.T) {
	obj := mustParse(t, "a = 1\nb = [1, 2]")
	wrapper := struct {
		Name   string  `json:"name"`
		Config *Object `json:"config"`
	}{Name: "x", Config: obj}

	out, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"x","config":{"a":1,"b":[1,2]}}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestObjectUserDataAndPriority(t *testing.T) {
	type payload struct{ N int }
	o := NewUserData(&payload{N: 3})
	if o.Type() != TypeUserData {
		t.Fatalf("type %s", o.Type())
	}
	if got, ok := o.UserData().(*payload); !ok || got.N != 3 {
		t.Errorf("user data is %#v", o.UserData())
	}
	// User data has no representation in any output format.
	if got := mustJSON(t, o); got != "null" {
		t.Errorf("emitted as %s", got)
	}

	o.SetPriority(3)
	if o.Priority() != 3 {
		t.Errorf("priority is %d", o.Priority())
	}
	o.SetPriority(99)
	if o.Priority() != MaxPriority {
		t.Errorf("priority %d was not clamped to %d", o.Priority(), MaxPriority)
	}
}

func TestOrAccessors(t *testing.T) {
	o := mustParse(t, "i = 1\nf = 1.5\ns = text\nb = yes")
	if got := o.Get("f").FloatOr(9); got != 1.5 {
		t.Errorf("got %v", got)
	}
	if got := o.Get("missing").FloatOr(9); got != 9 {
		t.Errorf("got %v", got)
	}
	if got := o.Get("s").FloatOr(9); got != 9 {
		t.Errorf("a string is not a number, got %v", got)
	}
	if !o.Get("i").BoolOr(false) {
		t.Error("a non zero number is true")
	}
	if !o.Get("s").BoolOr(true) {
		t.Error("a string is not convertible, so the default stands")
	}
	if got := o.Get("b").IntOr(9); got != 1 {
		t.Errorf("a true boolean is one, got %d", got)
	}
}
