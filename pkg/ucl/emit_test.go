package ucl

import (
	"testing"
)

const emitSource = `a = 1
b = [1, 2]
c { d = "x" }
e = null
f = 1.5
g = true
`

func TestEmitJSON(t *testing.T) {
	want := `{
    "a": 1,
    "b": [
        1,
        2
    ],
    "c": {
        "d": "x"
    },
    "e": null,
    "f": 1.5,
    "g": true
}
`
	got, err := mustParse(t, emitSource).Emit(JSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitConfig(t *testing.T) {
	want := `"a" = 1;
"b" [
    1,
    2,
]
"c" {
    "d" = "x";
}
"e" = null;
"f" = 1.5;
"g" = true;
`
	got, err := mustParse(t, emitSource).Emit(Config)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitYAML(t *testing.T) {
	want := `a: 1
b:
  - 1
  - 2
c:
  d: x
e: null
f: 1.5
g: true
`
	got, err := mustParse(t, emitSource).Emit(YAML)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitEmptyContainers(t *testing.T) {
	o := mustParse(t, "a = []\nb = {}\n")
	for _, tc := range []struct {
		f    Format
		want string
	}{
		{JSONCompact, `{"a":[],"b":{}}`},
		{Config, "\"a\" []\n\"b\" {\n}\n"},
		{YAML, "a: []\nb: {}\n"},
	} {
		got, err := o.Emit(tc.f)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.f, got, tc.want)
		}
	}
}

// TestConfigRoundTrip checks that what the config emitter produces parses
// back into the same document.
func TestConfigRoundTrip(t *testing.T) {
	sources := []string{
		emitSource,
		"a = 1\na = 2\n",
		`s { t { u = [1, [2, 3], {v = 4}] } }`,
		"key = \"quotes \\\" and \\\\ and \\n\"\n",
		"heredoc = <<EOD\nmulti\nline\nEOD\n",
		"time = 10s\nsize = 1kb\n",
		`unicode = "äöü ∑"`,
	}
	for _, src := range sources {
		o := mustParse(t, src)
		conf, err := o.Emit(Config)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseString(string(conf))
		if err != nil {
			t.Fatalf("reparsing %q failed: %v", conf, err)
		}
		if a, b := mustJSON(t, o), mustJSON(t, again); a != b {
			t.Errorf("round trip of %q changed the document:\n%s\n%s", src, a, b)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	o := mustParse(t, emitSource)
	js, err := o.Emit(JSON)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseString(string(js))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := mustJSON(t, o), mustJSON(t, again); a != b {
		t.Errorf("%s != %s", a, b)
	}
}

func TestMsgPackRoundTrip(t *testing.T) {
	o := mustParse(t, emitSource+"\nlong = \""+string(make([]byte, 0))+"\"\n")
	packed, err := o.Emit(MsgPack)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseMsgPack(packed)
	if err != nil {
		t.Fatal(err)
	}
	// Time values become plain floats after a msgpack round trip, which is
	// what libucl does as well.
	if a, b := mustJSON(t, o), mustJSON(t, again); a != b {
		t.Errorf("%s != %s", a, b)
	}
}

func TestMsgPackLargeValues(t *testing.T) {
	obj := NewObject()
	big := make([]byte, 70000)
	for i := range big {
		big[i] = 'x'
	}
	obj.Set("big", NewString(string(big)))
	arr := NewArray()
	for i := 0; i < 1000; i++ {
		arr.AppendElem(NewInt(int64(i)))
	}
	obj.Set("arr", arr)
	obj.Set("neg", NewInt(-70000))
	obj.Set("small", NewInt(-5))

	packed, err := obj.Emit(MsgPack)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseMsgPack(packed)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := mustJSON(t, obj), mustJSON(t, back); a != b {
		t.Error("large msgpack round trip changed the document")
	}
}

func TestStringEscaping(t *testing.T) {
	o := NewObject()
	o.Set("k", NewString("tab\there\x01 \"quoted\""))
	got := mustJSON(t, o)
	want := `{"k":"tab\there\u0001 \"quoted\""}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestYAMLQuoting(t *testing.T) {
	o := NewObject()
	o.Set("a", NewString("yes"))
	o.Set("b", NewString("123"))
	o.Set("c", NewString("plain text"))
	o.Set("d", NewString("- dash"))
	got, err := o.Emit(YAML)
	if err != nil {
		t.Fatal(err)
	}
	want := "a: \"yes\"\nb: \"123\"\nc: plain text\nd: \"- dash\"\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
