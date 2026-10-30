package ucl

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, src string, opts ...Option) *Object {
	t.Helper()
	o, err := ParseString(src, opts...)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return o
}

func mustJSON(t *testing.T, o *Object) string {
	t.Helper()
	b, err := o.Emit(JSONCompact)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return string(b)
}

func TestBasicSyntax(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `{}`},
		{"equals", "key = value", `{"key":"value"}`},
		{"colon", "key: value", `{"key":"value"}`},
		{"no separator", "key value", `{"key":"value"}`},
		{"semicolon", "a = 1; b = 2;", `{"a":1,"b":2}`},
		{"comma", "a = 1, b = 2", `{"a":1,"b":2}`},
		{"newline", "a = 1\nb = 2\n", `{"a":1,"b":2}`},
		{"nested", "s { k = v }", `{"s":{"k":"v"}}`},
		{"nested equals", "s = { k = v }", `{"s":{"k":"v"}}`},
		{"deep", "a { b { c { d = 1 } } }", `{"a":{"b":{"c":{"d":1}}}}`},
		{"array", "a = [1, 2, 3]", `{"a":[1,2,3]}`},
		{"array newline", "a = [\n1\n2\n]", `{"a":[1,2]}`},
		{"array of objects", "a = [{b=1},{c=2}]", `{"a":[{"b":1},{"c":2}]}`},
		{"empty array", "a = []", `{"a":[]}`},
		{"empty object", "a = {}", `{"a":{}}`},
		{"quoted key", `"a b" = 1`, `{"a b":1}`},
		{"spaces in value", "a = hello world", `{"a":"hello world"}`},
		{"top level braces", `{ "a": 1 }`, `{"a":1}`},
		{"top level array", `[1, 2]`, `[1,2]`},
		{"json", `{"a":[1,{"b":null}],"c":true}`, `{"a":[1,{"b":null}],"c":true}`},
		{"trailing comma", "a = [1, 2,]", `{"a":[1,2]}`},
		{"dotted key", "a.b = 1", `{"a.b":1}`},
		{"slash key", "/var/log = 1", `{"/var/log":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustJSON(t, mustParse(t, tc.in)); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComments(t *testing.T) {
	src := `
# a hash comment
a = 1 # trailing comment
/* a block comment
   spanning lines */
b = 2
/* nested /* comments */ are allowed */
c = 3
`
	if got := mustJSON(t, mustParse(t, src)); got != `{"a":1,"b":2,"c":3}` {
		t.Errorf("got %s", got)
	}
}

func TestUnterminatedComment(t *testing.T) {
	if _, err := ParseString("a = 1\n/* oops\n"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStrings(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"double quoted", `a = "hello"`, "hello"},
		{"escapes", `a = "x\ty\nz\"q\\"`, "x\ty\nz\"q\\"},
		{"unicode", `a = "\u00e4\u00f6"`, "äö"},
		{"surrogate pair", `a = "\ud83d\ude00"`, "\U0001F600"},
		{"escape drops backslash", `a = "a\qb"`, "aqb"},
		{"single quoted", `a = 'no \n escapes'`, `no \n escapes`},
		{"single quote escape", `a = 'it\'s'`, "it's"},
		{"slash escape", `a = "a\/b"`, "a/b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mustParse(t, tc.in).Get("a").Str()
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHeredoc(t *testing.T) {
	src := "key = <<EOD\nline one\nline two\nEOD\nnext = 1\n"
	o := mustParse(t, src)
	if got := o.Get("key").Str(); got != "line one\nline two" {
		t.Errorf("got %q", got)
	}
	if o.Get("key").Flags()&FlagMultiline == 0 {
		t.Error("multiline flag not set")
	}
	if o.Get("next").Int() != 1 {
		t.Error("parsing did not continue after the heredoc")
	}
}

func TestHeredocNotTerminated(t *testing.T) {
	if _, err := ParseString("key = <<EOD\nbody\n"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLessThanIsNotAHeredoc(t *testing.T) {
	o := mustParse(t, "key = << not a heredoc")
	if got := o.Get("key").Str(); got != "<< not a heredoc" {
		t.Errorf("got %q", got)
	}
}

func TestNumbers(t *testing.T) {
	tests := []struct {
		in   string
		typ  Type
		want any
	}{
		{"1", TypeInt, int64(1)},
		{"-1", TypeInt, int64(-1)},
		{"0x1F", TypeInt, int64(31)},
		{"0x1f", TypeInt, int64(31)},
		{"1.5", TypeFloat, 1.5},
		{"1e3", TypeFloat, 1000.0},
		{"-2.5e-2", TypeFloat, -0.025},
		{"1k", TypeInt, int64(1000)},
		{"1kb", TypeInt, int64(1024)},
		{"1m", TypeInt, int64(1000000)},
		{"1mb", TypeInt, int64(1048576)},
		{"1g", TypeInt, int64(1000000000)},
		{"1gb", TypeInt, int64(1073741824)},
		{"1ks", TypeTime, 1000.0},
		{"1.5k", TypeFloat, 1500.0},
		// libucl truncates a fractional value before a binary multiplier.
		{"1.5kb", TypeInt, int64(1024)},
		{"10s", TypeTime, 10.0},
		{"500ms", TypeTime, 0.5},
		{"2min", TypeTime, 120.0},
		{"1h", TypeTime, 3600.0},
		{"1d", TypeTime, 86400.0},
		{"1w", TypeTime, 604800.0},
		{"1y", TypeTime, 31536000.0},
		// These are not numbers and stay strings.
		{"1.2.3", TypeString, "1.2.3"},
		{"10 20", TypeString, "10 20"},
		{"1kx", TypeString, "1kx"},
		{"127.0.0.1", TypeString, "127.0.0.1"},
		{"v1", TypeString, "v1"},
		// libucl only tries a number when the token starts with a digit
		// or a minus sign, and it knows no standalone byte suffix.
		{"+3", TypeString, "+3"},
		{"10b", TypeString, "10b"},
		{".5", TypeString, ".5"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			v := mustParse(t, "a = "+tc.in).Get("a")
			if v.Type() != tc.typ {
				t.Fatalf("type %s, want %s", v.Type(), tc.typ)
			}
			switch want := tc.want.(type) {
			case int64:
				if v.Int() != want {
					t.Errorf("got %d, want %d", v.Int(), want)
				}
			case float64:
				if v.Float() != want {
					t.Errorf("got %v, want %v", v.Float(), want)
				}
			case string:
				if v.Str() != want {
					t.Errorf("got %q, want %q", v.Str(), want)
				}
			}
		})
	}
}

func TestNoTimeOption(t *testing.T) {
	v := mustParse(t, "a = 10s", WithoutTime()).Get("a")
	if v.Type() != TypeString || v.Str() != "10s" {
		t.Errorf("got %s %q", v.Type(), v.Str())
	}
}

func TestBooleansAndNull(t *testing.T) {
	for _, s := range []string{"true", "TRUE", "yes", "on", "On"} {
		if v := mustParse(t, "a = "+s).Get("a"); v.Type() != TypeBoolean || !v.Bool() {
			t.Errorf("%q did not parse as true", s)
		}
	}
	for _, s := range []string{"false", "False", "no", "off", "OFF"} {
		if v := mustParse(t, "a = "+s).Get("a"); v.Type() != TypeBoolean || v.Bool() {
			t.Errorf("%q did not parse as false", s)
		}
	}
	if v := mustParse(t, "a = null").Get("a"); v.Type() != TypeNull {
		t.Errorf("null parsed as %s", v.Type())
	}
}

func TestImplicitArray(t *testing.T) {
	o := mustParse(t, "a = 1\na = 2\na = 3\n")
	if got := mustJSON(t, o); got != `{"a":[1,2,3]}` {
		t.Fatalf("got %s", got)
	}
	v := o.Get("a")
	if v.Flags()&FlagMultiValue == 0 {
		t.Error("multi value flag not set")
	}
	if n := len(o.Values("a")); n != 3 {
		t.Errorf("Values returned %d items", n)
	}
	// An explicit array is reported as a single value.
	o = mustParse(t, "a = [1, 2]")
	if n := len(o.Values("a")); n != 1 {
		t.Errorf("Values returned %d items for an explicit array", n)
	}
}

func TestImplicitArrayOfSections(t *testing.T) {
	o := mustParse(t, "s { a = 1 }\ns { a = 2 }\n")
	if got := mustJSON(t, o); got != `{"s":[{"a":1},{"a":2}]}` {
		t.Fatalf("got %s", got)
	}
}

func TestNoImplicitArrays(t *testing.T) {
	o := mustParse(t, "a = 1\na = 2\n", WithoutImplicitArrays())
	if got := mustJSON(t, o); got != `{"a":2}` {
		t.Fatalf("got %s", got)
	}
}

func TestDuplicateStrategies(t *testing.T) {
	src := "s { a = 1 }\ns { b = 2 }\n"
	tests := []struct {
		strategy Duplicate
		want     string
	}{
		{DuplicateAppend, `{"s":[{"a":1},{"b":2}]}`},
		{DuplicateMerge, `{"s":{"a":1,"b":2}}`},
		{DuplicateRewrite, `{"s":{"b":2}}`},
	}
	for _, tc := range tests {
		t.Run(tc.strategy.String(), func(t *testing.T) {
			o := mustParse(t, src, WithDuplicateStrategy(tc.strategy))
			if got := mustJSON(t, o); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
	if _, err := ParseString(src, WithDuplicateStrategy(DuplicateError)); err == nil {
		t.Error("DuplicateError did not report an error")
	}
}

func TestPriority(t *testing.T) {
	p := NewParser()
	if err := p.AddString("a = 1"); err != nil {
		t.Fatal(err)
	}
	WithPriority(5)(p)
	if err := p.AddString("a = 2"); err != nil {
		t.Fatal(err)
	}
	if got := p.Result().Get("a").Int(); got != 2 {
		t.Errorf("higher priority did not win, got %d", got)
	}
	WithPriority(1)(p)
	if err := p.AddString("a = 3"); err != nil {
		t.Fatal(err)
	}
	if got := p.Result().Get("a").Int(); got != 2 {
		t.Errorf("lower priority overrode a higher one, got %d", got)
	}
}

func TestPriorityMacro(t *testing.T) {
	o := mustParse(t, ".priority 4\na = 1\n")
	if got := o.Get("a").Priority(); got != 4 {
		t.Errorf("priority is %d", got)
	}
}

func TestVariables(t *testing.T) {
	o := mustParse(t, `a = "${FOO} and $BAR"`+"\nb = $FOO\nc = \"${MISSING}\"\n",
		WithVariable("FOO", "foo"), WithVariable("BAR", "bar"))
	if got := o.Get("a").Str(); got != "foo and bar" {
		t.Errorf("got %q", got)
	}
	if got := o.Get("b").Str(); got != "foo" {
		t.Errorf("got %q", got)
	}
	if got := o.Get("c").Str(); got != "${MISSING}" {
		t.Errorf("unknown variable was not preserved: %q", got)
	}
}

func TestVariableHandler(t *testing.T) {
	o := mustParse(t, `a = "${X}"`, WithVariableHandler(func(n string) (string, bool) {
		return "<" + n + ">", true
	}))
	if got := o.Get("a").Str(); got != "<X>" {
		t.Errorf("got %q", got)
	}
}

func TestNoVariablesInSingleQuotes(t *testing.T) {
	o := mustParse(t, `a = '$FOO'`, WithVariable("FOO", "foo"))
	if got := o.Get("a").Str(); got != "$FOO" {
		t.Errorf("got %q", got)
	}
}

func TestLowercaseKeys(t *testing.T) {
	o := mustParse(t, "Key = 1\nSECTION { Sub = 2 }", WithLowercaseKeys())
	if got := mustJSON(t, o); got != `{"key":1,"section":{"sub":2}}` {
		t.Errorf("got %s", got)
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, src := range []string{
		`a = "unterminated`,
		`a = 'unterminated`,
		`{ a = 1`,
		`a = [1, 2`,
		`}`,
		`a = "\uZZZZ"`,
	} {
		if _, err := ParseString(src); err == nil {
			t.Errorf("%q parsed without an error", src)
		}
	}
}

func TestErrorPosition(t *testing.T) {
	_, err := ParseFile("../../testdata/does-not-exist.conf")
	if err == nil {
		t.Fatal("expected an error")
	}
	_, err = ParseString("a = 1\nb = \"oops\nc = 2")
	if err == nil {
		t.Fatal("expected an error")
	}
	var se *SyntaxError
	if !asSyntaxError(err, &se) {
		t.Fatalf("error is %T, want *SyntaxError", err)
	}
	if se.Line != 2 {
		t.Errorf("error reported on line %d, want 2", se.Line)
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unexpected message %q", err)
	}
}

func asSyntaxError(err error, target **SyntaxError) bool {
	se, ok := err.(*SyntaxError)
	if ok {
		*target = se
	}
	return ok
}

func TestDurationAccessor(t *testing.T) {
	o := mustParse(t, "t = 1h30min")
	if o.Get("t").Type() != TypeString {
		t.Skip("compound durations are not a UCL feature")
	}
	o = mustParse(t, "t = 90min")
	if got := o.Get("t").Duration(); got != 90*time.Minute {
		t.Errorf("got %v", got)
	}
}

func TestMacrosDisabled(t *testing.T) {
	o := mustParse(t, ".include = 1", WithoutMacros())
	if o.Get(".include").Int() != 1 {
		t.Errorf("got %s", mustJSON(t, o))
	}
}

func TestUnknownMacro(t *testing.T) {
	if _, err := ParseString(`.frobnicate "x"`); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUserMacro(t *testing.T) {
	o := mustParse(t, `.upper "hello"`, WithMacro("upper", func(ctx *MacroContext) error {
		return ctx.Target.Insert("upper", NewString(strings.ToUpper(ctx.Value.ToString())), DuplicateRewrite)
	}))
	if got := o.Get("upper").Str(); got != "HELLO" {
		t.Errorf("got %q", got)
	}
}

func TestPkgConfLikeDocument(t *testing.T) {
	src := `
ABI = "FreeBSD:14:amd64";
REPOS_DIR [
    "/etc/pkg",
    "/usr/local/etc/pkg/repos",
]
PKG_ENV {
    http_proxy = "http://proxy.example.net:3128";
}
FreeBSD: {
    url: "pkg+https://pkg.FreeBSD.org/${ABI}/quarterly",
    mirror_type: "srv",
    signature_type: "fingerprints",
    fingerprints: "/usr/share/keys/pkg",
    enabled: yes
}
FETCH_TIMEOUT = 30s;
DEVELOPER_MODE = false;
`
	o := mustParse(t, src, WithVariable("ABI", "FreeBSD:14:amd64"))
	if got := o.Get("ABI").Str(); got != "FreeBSD:14:amd64" {
		t.Errorf("ABI is %q", got)
	}
	if got := o.Lookup("REPOS_DIR.1").Str(); got != "/usr/local/etc/pkg/repos" {
		t.Errorf("REPOS_DIR[1] is %q", got)
	}
	if got := o.Lookup("FreeBSD.url").Str(); got != "pkg+https://pkg.FreeBSD.org/FreeBSD:14:amd64/quarterly" {
		t.Errorf("url is %q", got)
	}
	if !o.Lookup("FreeBSD.enabled").Bool() {
		t.Error("FreeBSD repository is not enabled")
	}
	if got := o.Get("FETCH_TIMEOUT").Duration(); got != 30*time.Second {
		t.Errorf("timeout is %v", got)
	}
	if o.Get("DEVELOPER_MODE").Bool() {
		t.Error("developer mode should be false")
	}
}

// TestBareValueTermination pins down the UCL_CHARACTER_VALUE_END class of
// libucl: an unquoted value may contain spaces and parentheses but ends at a
// comment, at , ; and at an unbalanced ] or }.
func TestBareValueTermination(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"a = foo bar baz", "foo bar baz"},
		{"a = foo#bar", "foo"},
		{"a = foo # bar", "foo"},
		{"a = foo/*bar*/", "foo"},
		{"a = foo/bar", "foo/bar"},
		{"a = f(x)", "f(x)"},
		{"a = foo;", "foo"},
		{"a = foo,", "foo"},
		{"a = a[1]b", "a[1]b"},
		{"a = x{y}", "x{y}"},
		{"a = trailing   ", "trailing"},
		{`a = one\,two`, "one,two"},
		{`a = hash\#mark`, "hash#mark"},
		{`a = tab\there`, "tab\there"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := mustParse(t, tc.in).Get("a").Str(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBareValueInsideBraces(t *testing.T) {
	o := mustParse(t, "s { a = foo bar }")
	if got := o.Lookup("s.a").Str(); got != "foo bar" {
		t.Errorf("got %q", got)
	}
}

func TestNanAndInf(t *testing.T) {
	o := mustParse(t, "a = nan\nb = inf\nc = -inf\n")
	if !math.IsNaN(o.Get("a").Float()) {
		t.Errorf("nan is %v", o.Get("a").Float())
	}
	if !math.IsInf(o.Get("b").Float(), 1) {
		t.Errorf("inf is %v", o.Get("b").Float())
	}
	if !math.IsInf(o.Get("c").Float(), -1) {
		t.Errorf("-inf is %v", o.Get("c").Float())
	}
}

func TestMacroParameterValuesStopAtParenthesis(t *testing.T) {
	var got *Object
	_, err := ParseString(`.probe(a=1, b=two, c=yes) "value"`,
		WithMacro("probe", func(ctx *MacroContext) error {
			got = ctx.Params
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":1,"b":"two","c":true}`; mustJSON(t, got) != want {
		t.Errorf("got %s, want %s", mustJSON(t, got), want)
	}
}

func TestHeredocTerminatorRules(t *testing.T) {
	// The terminator may be followed by a separator instead of a newline.
	o := mustParse(t, "a = <<EOD\nbody\nEOD;\nb = 1\n")
	if got := o.Get("a").Str(); got != "body" {
		t.Errorf("got %q", got)
	}
	if o.Get("b").Int() != 1 {
		t.Error("parsing did not continue after the terminator")
	}

	// A word that only looks like the terminator does not end the string.
	o = mustParse(t, "a = <<EOD\nEODX\nEOD\n")
	if got := o.Get("a").Str(); got != "EODX" {
		t.Errorf("got %q", got)
	}

	// An empty body is allowed.
	o = mustParse(t, "a = <<EOD\nEOD\n")
	if got := o.Get("a").Str(); got != "" {
		t.Errorf("got %q", got)
	}

	// libucl only accepts upper case terminators, so this is a plain value.
	o = mustParse(t, "a = <<eod\n")
	if got := o.Get("a").Str(); got != "<<eod" {
		t.Errorf("got %q", got)
	}
}

func TestSingleQuotedEscapes(t *testing.T) {
	// Only \' and a line continuation have a meaning, everything else stays
	// literal, including a doubled backslash.
	o := mustParse(t, `a = 'back\\slash'`+"\nb = 'line\\\ncontinued'\n")
	if got := o.Get("a").Str(); got != `back\\slash` {
		t.Errorf("got %q", got)
	}
	if got := o.Get("b").Str(); got != "linecontinued" {
		t.Errorf("got %q", got)
	}
}

// TestUnicodeEscapes pins the \uXXXX decoder, including the handling of a
// surrogate half that stands on its own. libucl encodes each half separately
// and produces invalid UTF-8; this package combines a pair and replaces a
// stray half, which is a documented deviation.
func TestUnicodeEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ascii", `a = "A"`, "A"},
		{"lower hex", `a = "ä"`, "ä"},
		{"upper hex", `a = "ä"`, "ä"},
		{"mixed hex", `a = "ï"`, "ï"},
		{"pair", `a = "😀"`, "\U0001F600"},
		{"two pairs", `a = "😀😀"`, "\U0001F600\U0001F600"},
		// A half on its own becomes the replacement character, and what
		// follows it must survive rather than be swallowed.
		{"lone low half", `a = "\udc00A"`, "�A"},
		{"lone high half", `a = "\ud800A"`, "�A"},
		{"high half then text", `a = "\ud83dZ"`, "�Z"},
		{"high half at end", `a = "\ud83d"`, "�"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustParse(t, tc.in).Get("a").Str(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnicodeEscapeErrors(t *testing.T) {
	for _, src := range []string{
		`a = "\u12"`,
		`a = "\uzzzz"`,
		`a = "\u"`,
		`a = "\ud83d\uzzzz"`,
	} {
		if _, err := ParseString(src); err == nil {
			t.Errorf("%s parsed without an error", src)
		}
	}
}

// The remaining escapes of a double quoted string.
func TestControlEscapes(t *testing.T) {
	o := mustParse(t, `a = "cr\rbs\bff\f"`)
	if got := o.Get("a").Str(); got != "cr\rbs\bff\f" {
		t.Errorf("got %q", got)
	}
	if _, err := ParseString(`a = "truncated\`); err == nil {
		t.Error("a string ending in a backslash should not parse")
	}
}

func TestQuotedKeys(t *testing.T) {
	o := mustParse(t, "'single quoted key' = 1\n\"double quoted key\" = 2\n")
	if o.Get("single quoted key").Int() != 1 || o.Get("double quoted key").Int() != 2 {
		t.Errorf("got %s", mustJSON(t, o))
	}
}

func TestHeredocWithCarriageReturns(t *testing.T) {
	o := mustParse(t, "a = <<EOD\r\nbody\r\nEOD\r\n")
	if got := o.Get("a").Str(); got != "body\r" {
		t.Errorf("got %q", got)
	}
}

// Hexadecimal literals and the special float names.
func TestNumberLiterals(t *testing.T) {
	tests := []struct {
		in   string
		typ  Type
		want any
	}{
		{"0xff", TypeInt, int64(255)},
		{"0XFF", TypeInt, int64(255)},
		{"-0x10", TypeInt, int64(-16)},
		{"0x7fffffffffffffff", TypeInt, int64(math.MaxInt64)},
		// Not numbers, so they stay strings.
		{"0x", TypeString, "0x"},
		{"0xzz", TypeString, "0xzz"},
		{"0xffffffffffffffffff", TypeString, "0xffffffffffffffffff"},
		{"1kx", TypeString, "1kx"},
		{"1ss", TypeString, "1ss"},
		// Suffixes that only exist because libucl has them.
		{"2gs", TypeTime, 2e9},
		{"3ks", TypeTime, 3000.0},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			v := mustParse(t, "a = "+tc.in).Get("a")
			if v.Type() != tc.typ {
				t.Fatalf("type %s, want %s", v.Type(), tc.typ)
			}
			switch want := tc.want.(type) {
			case int64:
				if v.Int() != want {
					t.Errorf("got %d, want %d", v.Int(), want)
				}
			case float64:
				if v.Float() != want {
					t.Errorf("got %v, want %v", v.Float(), want)
				}
			case string:
				if v.Str() != want {
					t.Errorf("got %q, want %q", v.Str(), want)
				}
			}
		})
	}
}

// A number too large for an int falls back to a float rather than wrapping.
func TestNumberOverflowBecomesFloat(t *testing.T) {
	v := mustParse(t, "a = 99999999999999999999").Get("a")
	if v.Type() != TypeFloat {
		t.Errorf("type %s, want float", v.Type())
	}
	if v.Float() != 1e20 {
		t.Errorf("got %v", v.Float())
	}
}

func TestParserOptionsWithoutExamples(t *testing.T) {
	// WithVariables and SetVariable both feed the same table.
	p := NewParser(WithVariables(map[string]string{"A": "1", "B": "2"}))
	p.SetVariable("C", "3")
	if err := p.AddString(`v = "${A}${B}${C}"`); err != nil {
		t.Fatal(err)
	}
	if got := p.Result().Get("v").Str(); got != "123" {
		t.Errorf("got %q", got)
	}

	// RegisterMacro after construction behaves like WithMacro.
	p = NewParser()
	p.RegisterMacro("keep", func(ctx *MacroContext) error {
		return ctx.Target.Insert("kept", ctx.Value, DuplicateRewrite)
	})
	if err := p.AddChunk([]byte(`.keep "yes"`)); err != nil {
		t.Fatal(err)
	}
	if got := p.Result().Get("kept").Str(); got != "yes" {
		t.Errorf("got %q", got)
	}
}

func TestWithoutFileVariables(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.conf", `here = "${CURDIR}"`+"\n")

	o, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if o.Get("here").Str() == "${CURDIR}" {
		t.Fatal("CURDIR should normally be expanded")
	}

	o, err = ParseFile(path, WithoutFileVariables())
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Get("here").Str(); got != "${CURDIR}" {
		t.Errorf("CURDIR was expanded despite WithoutFileVariables: %q", got)
	}
}

// The depth limit is reached by nesting, which the loop detector cannot catch
// because every file in the chain is different.
func TestWithMaxIncludeDepth(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.conf", "deep = 1\n")
	write(t, dir, "b.conf", `.include "c.conf"`+"\n")
	write(t, dir, "a.conf", `.include "b.conf"`+"\n")

	if _, err := ParseFile(filepath.Join(dir, "a.conf")); err != nil {
		t.Fatalf("the chain should parse at the default depth: %v", err)
	}
	_, err := ParseFile(filepath.Join(dir, "a.conf"), WithMaxIncludeDepth(1))
	if err == nil || !strings.Contains(err.Error(), "nested deeper") {
		t.Errorf("got %v", err)
	}
}
