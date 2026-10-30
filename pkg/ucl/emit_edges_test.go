package ucl

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// The format names are user visible: cmd/ucl accepts and prints them.
func TestFormatString(t *testing.T) {
	for _, tc := range []struct {
		f    Format
		want string
	}{
		{JSON, "json"},
		{JSONCompact, "json-compact"},
		{Config, "config"},
		{YAML, "yaml"},
		{MsgPack, "msgpack"},
	} {
		if got := tc.f.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestTypeAndDuplicateString(t *testing.T) {
	types := map[Type]string{
		TypeObject: "object", TypeArray: "array", TypeInt: "int",
		TypeFloat: "float", TypeString: "string", TypeBoolean: "boolean",
		TypeTime: "time", TypeUserData: "userdata", TypeNull: "null",
	}
	for typ, want := range types {
		if got := typ.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	dups := map[Duplicate]string{
		DuplicateAppend: "append", DuplicateMerge: "merge",
		DuplicateRewrite: "rewrite", DuplicateError: "error",
	}
	for dup, want := range dups {
		if got := dup.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestEmitUnknownFormat(t *testing.T) {
	_, err := NewObject().Emit(Format(99))
	if err == nil || !strings.Contains(err.Error(), "unknown emit format") {
		t.Errorf("got %v", err)
	}
}

// Control characters must be escaped rather than written through, and the
// result has to read back as the string that went in.
func TestEmitEscapesControlCharacters(t *testing.T) {
	ctl := "a\rb\bc\fd" + string(rune(0)) + "e" + string(rune(0x1f)) + "f"

	o := NewObject()
	o.Set("ctl", NewString(ctl))
	got := mustJSON(t, o)

	var into map[string]string
	if err := json.Unmarshal([]byte(got), &into); err != nil {
		t.Fatalf("the output is not valid JSON: %v\n%s", err, got)
	}
	if into["ctl"] != ctl {
		t.Errorf("the round trip changed the value to %q", into["ctl"])
	}
	for _, r := range []rune{'\r', '\b', '\f', 0, 0x1f} {
		if strings.ContainsRune(got, r) {
			t.Errorf("control character %U was emitted raw: %q", r, got)
		}
	}
}

// A string holding bytes that are not valid UTF-8 still has to come out as
// valid, parseable JSON.
func TestEmitInvalidUTF8(t *testing.T) {
	o := NewObject()
	o.Set("bad", NewString("ok"+string([]byte{0xff})+"end"))
	got := mustJSON(t, o)

	if !utf8.ValidString(got) {
		t.Errorf("the output is not valid UTF-8: %q", got)
	}
	var into map[string]string
	if err := json.Unmarshal([]byte(got), &into); err != nil {
		t.Errorf("the output is not valid JSON: %v\n%s", err, got)
	}
}

func TestEmitYAMLNesting(t *testing.T) {
	o := mustParse(t, `empties = [{}, []]
		matrix = [[1, 2], [3]]
		objects = [{a = 1}, {b = 2}]`)
	got, err := o.Emit(YAML)
	if err != nil {
		t.Fatal(err)
	}
	want := `empties:
  - {}
  - []
matrix:
  -
    - 1
    - 2
  -
    - 3
objects:
  -
    a: 1
  -
    b: 2
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitScalarDocument(t *testing.T) {
	o := mustParse(t, "a = 1").Get("a")
	for _, tc := range []struct {
		f    Format
		want string
	}{
		{JSONCompact, "1"},
		{YAML, "1\n"},
		{Config, "1\n"},
	} {
		out, err := o.Emit(tc.f)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.f, out, tc.want)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestEmitToFailingWriter(t *testing.T) {
	o := mustParse(t, "a = 1")
	if err := o.EmitTo(failingWriter{}, JSON); err == nil {
		t.Error("a failing writer should be reported")
	}
	if err := o.EmitTo(failingWriter{}, Format(99)); err == nil {
		t.Error("an unknown format should be reported before anything is written")
	}
}

// Merging an array with another value appends rather than replacing.
func TestMergeArrayValues(t *testing.T) {
	dst := mustParse(t, "a = [1, 2]\nb = [1]\nc = 1")
	src := mustParse(t, "a = [3]\nb = 2\nc = [3]")
	if err := dst.Merge(src, DuplicateMerge); err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, dst); got != `{"a":[1,2,3],"b":[1,2],"c":[3]}` {
		t.Errorf("got %s", got)
	}

	if err := dst.Merge(NewArray(), DuplicateMerge); err == nil {
		t.Error("merging an array into an object should be an error")
	}
	if err := NewArray().Merge(dst, DuplicateMerge); err == nil {
		t.Error("merging into an array should be an error")
	}
}

// Escapes are decoded in unquoted values too, not only in quoted strings.
func TestBareValueEscapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{`a = tab\there`, "tab\there"},
		{`a = nl\nhere`, "nl\nhere"},
		{`a = cr\rbs\bff\f`, "cr\rbs\bff\f"},
		{`a = quote\"inside`, `quote"inside`},
		{`a = back\\slash`, `back\slash`},
		{`a = unknown\qescape`, "unknownqescape"},
		{`a = trailing\`, `trailing\`},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := mustParse(t, tc.in).Get("a").Str(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
