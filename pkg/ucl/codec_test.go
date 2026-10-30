package ucl

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type repo struct {
	URL       string `ucl:"url"`
	Enabled   bool   `ucl:"enabled"`
	Signature string `ucl:"signature_type,omitempty"`
}

type config struct {
	ABI      string            `ucl:"ABI"`
	Repos    []string          `ucl:"REPOS_DIR"`
	Timeout  time.Duration     `ucl:"FETCH_TIMEOUT"`
	Env      map[string]string `ucl:"PKG_ENV"`
	FreeBSD  repo              `ucl:"FreeBSD"`
	Ignored  string            `ucl:"-"`
	Fallback int               `json:"fallback"`
}

const codecSource = `
ABI = "FreeBSD:14:amd64"
REPOS_DIR = ["/etc/pkg", "/usr/local/etc/pkg/repos"]
FETCH_TIMEOUT = 30s
PKG_ENV { http_proxy = "http://proxy:3128" }
FreeBSD {
    url = "pkg+https://example.org"
    enabled = yes
    signature_type = fingerprints
}
fallback = 7
`

func TestDecodeStruct(t *testing.T) {
	var c config
	if err := Unmarshal([]byte(codecSource), &c); err != nil {
		t.Fatal(err)
	}
	want := config{
		ABI:     "FreeBSD:14:amd64",
		Repos:   []string{"/etc/pkg", "/usr/local/etc/pkg/repos"},
		Timeout: 30 * time.Second,
		Env:     map[string]string{"http_proxy": "http://proxy:3128"},
		FreeBSD: repo{
			URL:       "pkg+https://example.org",
			Enabled:   true,
			Signature: "fingerprints",
		},
		Fallback: 7,
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
}

func TestDecodeImplicitArrayIntoSlice(t *testing.T) {
	var v struct {
		Listen []string `ucl:"listen"`
	}
	if err := Unmarshal([]byte("listen = a\nlisten = b\n"), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Listen, []string{"a", "b"}) {
		t.Errorf("got %q", v.Listen)
	}
}

func TestDecodeScalarIntoSlice(t *testing.T) {
	var v struct {
		Listen []string `ucl:"listen"`
	}
	if err := Unmarshal([]byte("listen = a\n"), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Listen, []string{"a"}) {
		t.Errorf("got %q", v.Listen)
	}
}

func TestDecodeCaseInsensitive(t *testing.T) {
	var v struct {
		Name string
	}
	if err := Unmarshal([]byte("NAME = bob"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Name != "bob" {
		t.Errorf("got %q", v.Name)
	}
}

func TestDecodeEmbedded(t *testing.T) {
	type base struct {
		A int `ucl:"a"`
	}
	type outer struct {
		base
		B int `ucl:"b"`
	}
	var v outer
	if err := Unmarshal([]byte("a = 1\nb = 2"), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 1 || v.B != 2 {
		t.Errorf("got %+v", v)
	}
}

func TestDecodeIntoInterface(t *testing.T) {
	var v map[string]any
	if err := Unmarshal([]byte("a = 1\nb { c = x }"), &v); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": int64(1), "b": map[string]any{"c": "x"}}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("got %#v", v)
	}
}

func TestDecodeObjectField(t *testing.T) {
	var v struct {
		Raw *Object `ucl:"raw"`
	}
	if err := Unmarshal([]byte("raw { a = 1 }"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Raw.Get("a").Int() != 1 {
		t.Error("raw object was not captured")
	}
}

func TestDecodeDurationForms(t *testing.T) {
	var v struct {
		A time.Duration `ucl:"a"`
		B time.Duration `ucl:"b"`
		C time.Duration `ucl:"c"`
	}
	if err := Unmarshal([]byte(`a = 5s
b = 90
c = "1500ms"`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 5*time.Second || v.B != 90*time.Second || v.C != 1500*time.Millisecond {
		t.Errorf("got %+v", v)
	}
}

func TestDecodeErrors(t *testing.T) {
	var v struct {
		A int `ucl:"a"`
	}
	if err := Unmarshal([]byte(`a = "not a number"`), &v); err == nil {
		t.Error("expected an error")
	}
	if err := Unmarshal([]byte(`a = 1`), v); err == nil {
		t.Error("decoding into a non pointer should fail")
	}
}

func TestEncodeStruct(t *testing.T) {
	c := struct {
		Name    string            `ucl:"name"`
		Count   int               `ucl:"count"`
		Ratio   float64           `ucl:"ratio"`
		On      bool              `ucl:"on"`
		Tags    []string          `ucl:"tags"`
		Env     map[string]string `ucl:"env"`
		Timeout time.Duration     `ucl:"timeout"`
		Skip    string            `ucl:"skip,omitempty"`
	}{
		Name:    "test",
		Count:   3,
		Ratio:   0.5,
		On:      true,
		Tags:    []string{"a", "b"},
		Env:     map[string]string{"b": "2", "a": "1"},
		Timeout: 90 * time.Second,
	}
	o, err := FromInterface(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"test","count":3,"ratio":0.5,"on":true,"tags":["a","b"],` +
		`"env":{"a":"1","b":"2"},"timeout":90.0}`
	if got := mustJSON(t, o); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	var c config
	if err := Unmarshal([]byte(codecSource), &c); err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(c, Config)
	if err != nil {
		t.Fatal(err)
	}
	var back config
	if err := Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, back) {
		t.Errorf("got %+v\nwant %+v", back, c)
	}
}

func TestInterfaceConversion(t *testing.T) {
	o := mustParse(t, "a = 1\nb = 1.5\nc = x\nd = true\ne = null\nf = 10s")
	m, ok := o.Interface().(map[string]any)
	if !ok {
		t.Fatal("not a map")
	}
	if m["a"] != int64(1) || m["b"] != 1.5 || m["c"] != "x" || m["d"] != true {
		t.Errorf("got %#v", m)
	}
	if m["e"] != nil {
		t.Errorf("null became %#v", m["e"])
	}
	if m["f"] != 10*time.Second {
		t.Errorf("time became %#v", m["f"])
	}
}

func TestObjectMutation(t *testing.T) {
	o := NewObject()
	o.Set("a", NewInt(1))
	o.Add("b", NewString("x"))
	o.Add("b", NewString("y"))
	if got := mustJSON(t, o); got != `{"a":1,"b":["x","y"]}` {
		t.Fatalf("got %s", got)
	}
	if !o.Delete("a") {
		t.Error("delete reported a missing key")
	}
	if o.Delete("a") {
		t.Error("delete reported a key twice")
	}
	o.Set("c", NewBool(false))
	if got := mustJSON(t, o); got != `{"b":["x","y"],"c":false}` {
		t.Errorf("got %s", got)
	}
	if got := o.Keys(); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("keys are %q", got)
	}
}

func TestCopyIsDeep(t *testing.T) {
	o := mustParse(t, "s { a = 1 }")
	c := o.Copy()
	c.Get("s").Set("a", NewInt(2))
	if o.Lookup("s.a").Int() != 1 {
		t.Error("Copy shared state with the original")
	}
}

func TestNilSafety(t *testing.T) {
	var o *Object
	if o.Get("x").Lookup("y.z").StrOr("fallback") != "fallback" {
		t.Error("chained lookups on nil are not safe")
	}
	if o.Len() != 0 || !o.IsNull() || o.Type() != TypeNull {
		t.Error("nil object does not behave like null")
	}
}

type inlineCreds struct {
	User string `ucl:"user"`
	Pass string `ucl:"password,omitempty"`
}

type inlineConf struct {
	Creds inlineCreds `ucl:",inline"`
	Host  string      `ucl:"host"`
}

// An inline field's contents live in the enclosing object, for decoding just
// as for encoding and validation.
func TestDecodeInline(t *testing.T) {
	var c inlineConf
	if err := Unmarshal([]byte("user = bob\npassword = s3cret\nhost = db"), &c); err != nil {
		t.Fatal(err)
	}
	want := inlineConf{Creds: inlineCreds{User: "bob", Pass: "s3cret"}, Host: "db"}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v, want %+v", c, want)
	}
}

func TestDecodeInlinePointer(t *testing.T) {
	type conf struct {
		Creds *inlineCreds `ucl:",inline"`
		Host  string       `ucl:"host"`
	}
	var c conf
	if err := Unmarshal([]byte("user = bob\nhost = db"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Creds == nil {
		t.Fatal("the inline pointer was not allocated")
	}
	if c.Creds.User != "bob" || c.Host != "db" {
		t.Errorf("got %+v", c)
	}
}

func TestDecodeInlineNested(t *testing.T) {
	type inner struct {
		A int `ucl:"a"`
	}
	type middle struct {
		Inner inner `ucl:",inline"`
		B     int   `ucl:"b"`
	}
	type outer struct {
		Middle middle `ucl:",inline"`
		C      int    `ucl:"c"`
	}
	var o outer
	if err := Unmarshal([]byte("a = 1\nb = 2\nc = 3"), &o); err != nil {
		t.Fatal(err)
	}
	if o.Middle.Inner.A != 1 || o.Middle.B != 2 || o.C != 3 {
		t.Errorf("got %+v", o)
	}
}

// Encoding, decoding and validation must agree about where an inline field's
// keys are, otherwise a document this package writes is one it cannot read.
func TestInlineRoundTrip(t *testing.T) {
	in := inlineConf{Creds: inlineCreds{User: "bob", Pass: "s3cret"}, Host: "db"}

	obj, err := FromInterface(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, obj); got != `{"user":"bob","password":"s3cret","host":"db"}` {
		t.Fatalf("encoded as %s", got)
	}
	if err := Validate(obj, inlineConf{}, RejectUnknownKeys()); err != nil {
		t.Fatalf("the encoded document does not validate: %v", err)
	}

	var back inlineConf
	if err := obj.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, back) {
		t.Errorf("got %+v, want %+v", back, in)
	}
}

// A type that is decoded as a whole keeps its own key even when it is tagged
// inline, because folding its fields away would make it undecodable.
func TestInlineIgnoredForOpaqueTypes(t *testing.T) {
	type conf struct {
		Raw  *Object       `ucl:"raw,inline"`
		Wait time.Duration `ucl:"wait,inline"`
		Name string        `ucl:"name,inline"`
	}
	var c conf
	if err := Unmarshal([]byte("raw { a = 1 }\nwait = 5s\nname = x"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Raw.Get("a").Int() != 1 || c.Wait != 5*time.Second || c.Name != "x" {
		t.Errorf("got %+v", c)
	}
}

type defaultsConf struct {
	Workers int           `ucl:"workers,default=4,min=1,max=64"`
	Timeout time.Duration `ucl:"timeout,default=30s"`
	MaxBody int64         `ucl:"max_body,default=8mb"`
	Mode    string        `ucl:"mode,default=safe,enum=fast|safe"`
	Debug   bool          `ucl:"debug,default=no"`
	Hosts   []string      `ucl:"hosts,default=[alpha, beta]"`
	Plain   string        `ucl:"plain"`
}

func TestDecodeDefaults(t *testing.T) {
	var c defaultsConf
	if err := Unmarshal([]byte("plain = x"), &c); err != nil {
		t.Fatal(err)
	}
	want := defaultsConf{
		Workers: 4,
		Timeout: 30 * time.Second,
		MaxBody: 8 * 1024 * 1024,
		Mode:    "safe",
		Debug:   false,
		Hosts:   []string{"alpha", "beta"},
		Plain:   "x",
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}
}

func TestDecodeDefaultsAreOverriddenByTheDocument(t *testing.T) {
	var c defaultsConf
	src := "workers = 16\ntimeout = 2min\nmode = fast\nhosts = [only]"
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.Workers != 16 || c.Timeout != 2*time.Minute || c.Mode != "fast" {
		t.Errorf("got %+v", c)
	}
	if !reflect.DeepEqual(c.Hosts, []string{"only"}) {
		t.Errorf("hosts is %q", c.Hosts)
	}
}

// A value the caller supplied beforehand wins over the declared default, so
// the two ways of expressing a default compose instead of fighting.
func TestDecodeDefaultsYieldToCallerValues(t *testing.T) {
	c := defaultsConf{Workers: 99, Mode: "fast"}
	if err := Unmarshal([]byte("plain = x"), &c); err != nil {
		t.Fatal(err)
	}
	if c.Workers != 99 {
		t.Errorf("workers is %d, want the caller's 99", c.Workers)
	}
	if c.Mode != "fast" {
		t.Errorf("mode is %q, want the caller's fast", c.Mode)
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("timeout is %v, want the declared default", c.Timeout)
	}
}

func TestDecodeDefaultsOnInlineFields(t *testing.T) {
	type inner struct {
		A int `ucl:"a,default=7"`
	}
	type outer struct {
		Inner inner `ucl:",inline"`
		B     int   `ucl:"b,default=8"`
	}
	var o outer
	if err := Unmarshal([]byte(""), &o); err != nil {
		t.Fatal(err)
	}
	if o.Inner.A != 7 || o.B != 8 {
		t.Errorf("got %+v", o)
	}
}

func TestDefaultTagErrors(t *testing.T) {
	type both struct {
		A int `ucl:"a,required,default=1"`
	}
	var b both
	err := Unmarshal([]byte(""), &b)
	if err == nil || !strings.Contains(err.Error(), "required and default cannot both be given") {
		t.Errorf("got %v", err)
	}
	if err := Validate(NewObject(), both{}); err == nil ||
		!strings.Contains(err.Error(), "required and default cannot both be given") {
		t.Errorf("validate reported %v", err)
	}

	type bad struct {
		A int `ucl:"a,default=not a number"`
	}
	var v bad
	if err := Unmarshal([]byte(""), &v); err == nil {
		t.Error("a default that does not fit the field should be an error")
	}
}
