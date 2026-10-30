package ucl

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type srvConfig struct {
	Name    string            `ucl:"name,required,min=1,max=32"`
	Workers int               `ucl:"workers,min=1,max=64"`
	Mode    string            `ucl:"mode,enum=fast|safe"`
	Timeout time.Duration     `ucl:"timeout,min=1s,max=10min"`
	Ratio   float64           `ucl:"ratio,min=0,max=1"`
	Chunk   int               `ucl:"chunk,multipleof=512"`
	Host    string            `ucl:"host,pattern=^[a-z0-9.-]+$"`
	Hosts   []string          `ucl:"hosts,min=1,max=3,unique"`
	Env     map[string]string `ucl:"env"`
	DB      struct {
		Port int `ucl:"port,required,min=1,max=65535"`
	} `ucl:"database"`
}

func validateString(t *testing.T, src string, v any, opts ...ValidateOption) error {
	t.Helper()
	return Validate(mustParse(t, src), v, opts...)
}

func TestValidateAccepts(t *testing.T) {
	src := `
		name = frontend
		workers = 4
		mode = fast
		timeout = 30s
		ratio = 0.5
		chunk = 4096
		host = db.example.net
		hosts = [a, b]
		env { TZ = UTC }
		database { port = 5432 }
	`
	if err := validateString(t, src, srvConfig{}); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	src := `
		workers = 99
		mode = quick
		timeout = 20min
		ratio = 2
		chunk = 1000
		host = "Not A Host"
		hosts = [a, a, b, c]
		database { }
	`
	err := validateString(t, src, srvConfig{})
	if err == nil {
		t.Fatal("invalid document accepted")
	}
	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error is %T, want ValidationErrors", err)
	}
	want := map[string]string{
		"name":          "is required",
		"workers":       "must be at most 64",
		"mode":          "must be one of fast, safe",
		"timeout":       "must be at most 10m0s",
		"ratio":         "must be at most 1",
		"chunk":         "must be a multiple of 512",
		"host":          "must match",
		"hosts":         "duplicate",
		"database.port": "is required",
	}
	got := make(map[string]string, len(errs))
	for _, e := range errs {
		got[e.Path] = e.Msg
	}
	for path, fragment := range want {
		msg, ok := got[path]
		if !ok {
			t.Errorf("no error reported for %s", path)
			continue
		}
		if !strings.Contains(msg, fragment) {
			t.Errorf("%s: got %q, want it to mention %q", path, msg, fragment)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d errors, want %d: %v", len(got), len(want), got)
	}
}

func TestValidateMinimums(t *testing.T) {
	src := `name = ""
		workers = 0
		timeout = 500ms
		hosts = []
		database { port = 5432 }`
	err := validateString(t, src, srvConfig{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		"name: must be at least 1 characters",
		"workers: must be at least 1",
		"timeout: must be at least 1s",
		"hosts: must be at least 1 items",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestValidateTypes(t *testing.T) {
	src := `name = ok
		workers = "a lot"
		ratio = { nested = 1 }
		database { port = 5432 }`
	err := validateString(t, src, srvConfig{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		`workers: expected an integer, got "a lot"`,
		"ratio: expected a float64, got object",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestValidateUnknownKeys(t *testing.T) {
	src := `name = ok
		wrokers = 4
		database { port = 5432, extra = 1 }`

	if err := validateString(t, src, srvConfig{}); err != nil {
		t.Fatalf("unknown keys should be allowed by default: %v", err)
	}
	err := validateString(t, src, srvConfig{}, RejectUnknownKeys())
	if err == nil {
		t.Fatal("expected unknown keys to be reported")
	}
	for _, want := range []string{"wrokers: unknown key", "database.extra: unknown key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestValidateImplicitArrayAsList(t *testing.T) {
	// A single value and a repeated key both satisfy a slice field.
	if err := validateString(t, "name = x\nhosts = a\ndatabase { port = 1 }", srvConfig{}); err != nil {
		t.Errorf("single value rejected: %v", err)
	}
	if err := validateString(t, "name = x\nhosts = a\nhosts = b\ndatabase { port = 1 }", srvConfig{}); err != nil {
		t.Errorf("implicit array rejected: %v", err)
	}
}

func TestValidatePointerAndEmbedded(t *testing.T) {
	type inner struct {
		A int `ucl:"a,required"`
	}
	type outer struct {
		inner
		B *int   `ucl:"b,min=2"`
		C string `ucl:"c"`
	}
	if err := validateString(t, "a = 1\nb = 3", outer{}); err != nil {
		t.Errorf("valid document rejected: %v", err)
	}
	err := validateString(t, "b = 1", outer{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"a: is required", "b: must be at least 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestValidateInlineStruct(t *testing.T) {
	type creds struct {
		User string `ucl:"user,required"`
	}
	type conf struct {
		Creds creds  `ucl:",inline"`
		Host  string `ucl:"host"`
	}
	if err := validateString(t, "user = bob\nhost = h", conf{}); err != nil {
		t.Errorf("inline fields rejected: %v", err)
	}
	if err := validateString(t, "user = bob\nhost = h", conf{}, RejectUnknownKeys()); err != nil {
		t.Errorf("inline field counted as unknown: %v", err)
	}
	if err := validateString(t, "host = h", conf{}); err == nil {
		t.Error("required inline field not enforced")
	}
}

func TestValidateBadTag(t *testing.T) {
	type bad struct {
		A int `ucl:"a,min=abc"`
	}
	err := validateString(t, "a = 1", bad{})
	if err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("got %v", err)
	}

	type unknown struct {
		A int `ucl:"a,mni=1"`
	}
	err = validateString(t, "a = 1", unknown{})
	if err == nil || !strings.Contains(err.Error(), `unknown option "mni"`) {
		t.Errorf("got %v", err)
	}
}

func TestValidateIgnoresJSONTagOptions(t *testing.T) {
	type conf struct {
		A int `json:"a,string,omitempty"`
	}
	if err := validateString(t, "a = 1", conf{}); err != nil {
		t.Errorf("json tag options should be ignored: %v", err)
	}
}

func TestValidatePatternWithBraces(t *testing.T) {
	type conf struct {
		V string `ucl:"v,pattern=^[a-z]{2,4}$"`
	}
	if err := validateString(t, "v = abc", conf{}); err != nil {
		t.Errorf("valid value rejected: %v", err)
	}
	if err := validateString(t, "v = abcdef", conf{}); err == nil {
		t.Error("pattern not enforced")
	}
}

func TestValidateArgumentChecks(t *testing.T) {
	if err := Validate(NewObject(), nil); err == nil {
		t.Error("nil accepted")
	}
	if err := Validate(NewObject(), 42); err == nil {
		t.Error("non struct accepted")
	}
	if err := Validate(NewObject(), &srvConfig{}); err == nil {
		t.Error("a pointer to a struct should work and report the missing keys")
	}
}

func TestValidateThenDecode(t *testing.T) {
	src := "name = x\nworkers = 8\ndatabase { port = 5432 }"
	obj := mustParse(t, src)
	if err := Validate(obj, srvConfig{}); err != nil {
		t.Fatal(err)
	}
	var cfg srvConfig
	if err := obj.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workers != 8 || cfg.DB.Port != 5432 {
		t.Errorf("got %+v", cfg)
	}
}

func TestValidateBoundsUseSuffixes(t *testing.T) {
	type conf struct {
		Body int64         `ucl:"body,max=64mb"`
		Wait time.Duration `ucl:"wait,max=1h"`
	}
	if err := validateString(t, "body = 8mb\nwait = 30min", conf{}); err != nil {
		t.Errorf("valid document rejected: %v", err)
	}
	err := validateString(t, "body = 128mb\nwait = 2h", conf{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"body: must be at most 67108864", "wait: must be at most 1h0m0s"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

// enum, pattern and multipleof describe a single value, so on a list they
// apply to each element rather than to the collection.
func TestValidateElementConstraints(t *testing.T) {
	type conf struct {
		Modes  []string          `ucl:"modes,enum=fast|safe"`
		Names  []string          `ucl:"names,pattern=^[a-z]+$"`
		Sizes  []int             `ucl:"sizes,multipleof=512"`
		Levels map[string]string `ucl:"levels,enum=debug|info"`
	}

	ok := `modes  = [fast, safe]
		names  = [alpha, beta]
		sizes  = [512, 1024]
		levels { app = debug, db = info }`
	if err := validateString(t, ok, conf{}); err != nil {
		t.Fatalf("a valid document was rejected: %v", err)
	}

	bad := `modes  = [fast, quick]
		names  = [alpha, "Beta 2"]
		sizes  = [512, 1000]
		levels { app = debug, db = chatty }`
	err := validateString(t, bad, conf{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		`modes[1]: must be one of fast, safe, got "quick"`,
		"names[1]: must match ^[a-z]+$",
		"sizes[1]: must be a multiple of 512",
		`levels.db: must be one of debug, info, got "chatty"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

// A single value stands for a list of one, so the element constraints still
// apply to it.
func TestValidateElementConstraintsOnScalar(t *testing.T) {
	type conf struct {
		Modes []string `ucl:"modes,enum=fast|safe"`
	}
	if err := validateString(t, "modes = fast", conf{}); err != nil {
		t.Errorf("a valid single value was rejected: %v", err)
	}
	err := validateString(t, "modes = quick", conf{})
	if err == nil || !strings.Contains(err.Error(), "must be one of fast, safe") {
		t.Errorf("got %v", err)
	}
}

// min, max and len keep counting the elements of a list rather than being
// applied to each one.
func TestValidateSizeStillCountsElements(t *testing.T) {
	type conf struct {
		Ports []int `ucl:"ports,min=2,max=3,enum=80|443|8080"`
	}
	if err := validateString(t, "ports = [80, 443]", conf{}); err != nil {
		t.Errorf("a valid document was rejected: %v", err)
	}
	err := validateString(t, "ports = [80]", conf{})
	if err == nil || !strings.Contains(err.Error(), "must be at least 2 items") {
		t.Errorf("got %v", err)
	}
	err = validateString(t, "ports = [80, 22]", conf{})
	if err == nil || !strings.Contains(err.Error(), "ports[1]: must be one of 80, 443, 8080") {
		t.Errorf("got %v", err)
	}
}

func TestValidateEnumOnIntSlice(t *testing.T) {
	type conf struct {
		Levels []int `ucl:"levels,enum=1|2|3"`
	}
	if err := validateString(t, "levels = [1, 3]", conf{}); err != nil {
		t.Errorf("a valid document was rejected: %v", err)
	}
	if err := validateString(t, "levels = [1, 9]", conf{}); err == nil {
		t.Error("expected an error for 9")
	}
}

// A default= value that its own field's constraints reject is a contradiction
// in the struct, so validation reports it whatever the document says.
func TestValidateDefaults(t *testing.T) {
	type conf struct {
		Workers int      `ucl:"workers,default=0,min=1,max=64"`
		Mode    string   `ucl:"mode,default=turbo,enum=fast|safe"`
		Host    string   `ucl:"host,default=NOPE,pattern=^[a-z]+$"`
		Count   int      `ucl:"count,default=abc"`
		Hosts   []string `ucl:"hosts,default=[alpha, Beta],pattern=^[a-z]+$"`
	}
	want := []string{
		"workers: invalid default: must be at least 1, got 0",
		`mode: invalid default: must be one of fast, safe, got "turbo"`,
		"host: invalid default: must match ^[a-z]+$",
		`count: invalid default: expected an integer, got "abc"`,
		"hosts[1]: invalid default: must match ^[a-z]+$",
	}

	// Absent from the document, so the defaults would be used.
	err := validateString(t, "", conf{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("missing %q in:\n%v", w, err)
		}
	}

	// Present in the document, so the defaults are unused. The contradiction
	// is still in the struct and is still reported.
	err = validateString(t, `workers = 4
		mode = fast
		host = db
		count = 1
		hosts = [alpha]`, conf{})
	if err == nil {
		t.Fatal("expected the defaults to be reported anyway")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("missing %q in:\n%v", w, err)
		}
	}
}

func TestValidateGoodDefaultsAreSilent(t *testing.T) {
	type conf struct {
		Workers int           `ucl:"workers,default=4,min=1,max=64"`
		Timeout time.Duration `ucl:"timeout,default=30s,min=1s,max=5min"`
		Mode    string        `ucl:"mode,default=safe,enum=fast|safe"`
		MaxBody int64         `ucl:"max_body,default=8mb,max=64mb"`
		Hosts   []string      `ucl:"hosts,default=[alpha, beta],min=1,unique"`
	}
	if err := validateString(t, "", conf{}); err != nil {
		t.Errorf("valid defaults were rejected: %v", err)
	}
	if err := validateString(t, "workers = 8", conf{}); err != nil {
		t.Errorf("valid defaults were rejected: %v", err)
	}

	// And the defaults really are what decoding produces.
	var c conf
	if err := Unmarshal([]byte(""), &c); err != nil {
		t.Fatal(err)
	}
	if c.Workers != 4 || c.Timeout != 30*time.Second || c.Mode != "safe" ||
		c.MaxBody != 8*1024*1024 || len(c.Hosts) != 2 {
		t.Errorf("got %+v", c)
	}
}

// Validation reports a value that cannot become the field's type at all, so a
// configuration mistake is named rather than surfacing later as a decode
// error with no path.
func TestValidateScalarTypeErrors(t *testing.T) {
	type conf struct {
		N int           `ucl:"n"`
		U uint          `ucl:"u"`
		F float64       `ucl:"f"`
		B bool          `ucl:"b"`
		D time.Duration `ucl:"d"`
		S string        `ucl:"s"`
	}
	tests := []struct{ src, want string }{
		{`n = "not a number"`, `n: expected an integer, got "not a number"`},
		{"n = 1.5", "n: expected an integer, got 1.5"},
		{"n { a = 1 }", "n: expected a int, got object"},
		{"u = -1", "u: must not be negative"},
		{`f = "not a number"`, `f: expected a number, got "not a number"`},
		{`b = "maybe"`, `b: expected a boolean, got "maybe"`},
		{"b = [1]", "b: expected a boolean, got array"},
		{`d = "not a duration"`, `d: expected a duration, got "not a duration"`},
		{"s { a = 1 }", "s: expected a string, got object"},
	}
	for _, tc := range tests {
		t.Run(tc.src, func(t *testing.T) {
			err := validateString(t, tc.src, conf{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A numeric string and the UCL boolean spellings are accepted, because the
// decoder accepts them too. Validation must not be stricter than decoding.
func TestValidateAcceptsWhatDecodeAccepts(t *testing.T) {
	type conf struct {
		N int           `ucl:"n"`
		B bool          `ucl:"b"`
		D time.Duration `ucl:"d"`
		T upperString   `ucl:"t"`
	}
	for _, src := range []string{`n = "42"`, "b = on", "d = 30s", "d = 90", `d = "1h"`, "t = anything"} {
		if err := validateString(t, src, conf{}); err != nil {
			t.Errorf("%s was rejected: %v", src, err)
		}
		var c conf
		if err := Unmarshal([]byte(src), &c); err != nil {
			t.Errorf("%s validated but did not decode: %v", src, err)
		}
	}
}

func TestValidateMultipleOfErrors(t *testing.T) {
	type onString struct {
		S string `ucl:"s,multipleof=2"`
	}
	err := validateString(t, "s = text", onString{})
	if err == nil || !strings.Contains(err.Error(), "multipleof applies to numbers") {
		t.Errorf("got %v", err)
	}

	type zero struct {
		N int `ucl:"n,multipleof=0"`
	}
	err = validateString(t, "n = 4", zero{})
	if err == nil || !strings.Contains(err.Error(), "must not be zero") {
		t.Errorf("got %v", err)
	}

	type bad struct {
		N int `ucl:"n,multipleof=abc"`
	}
	err = validateString(t, "n = 4", bad{})
	if err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("got %v", err)
	}

	// Fractional divisors must not fall foul of floating point division.
	type frac struct {
		F float64 `ucl:"f,multipleof=0.1"`
	}
	if err := validateString(t, "f = 0.3", frac{}); err != nil {
		t.Errorf("0.3 is a multiple of 0.1: %v", err)
	}
	if err := validateString(t, "f = 0.35", frac{}); err == nil {
		t.Error("0.35 is not a multiple of 0.1")
	}

	// A duration bound is written the way a document writes one.
	type dur struct {
		D time.Duration `ucl:"d,multipleof=30s"`
	}
	if err := validateString(t, "d = 2min", dur{}); err != nil {
		t.Errorf("2min is a multiple of 30s: %v", err)
	}
	if err := validateString(t, "d = 45s", dur{}); err == nil {
		t.Error("45s is not a multiple of 30s")
	}
}

func TestValidateBadBounds(t *testing.T) {
	type dur struct {
		D time.Duration `ucl:"d,min=soon"`
	}
	err := validateString(t, "d = 30s", dur{})
	if err == nil || !strings.Contains(err.Error(), "not a duration") {
		t.Errorf("got %v", err)
	}
}

func TestValidateMapField(t *testing.T) {
	type conf struct {
		M map[string]int `ucl:"m,min=1"`
	}
	if err := validateString(t, "m { a = 1 }", conf{}); err != nil {
		t.Errorf("a valid map was rejected: %v", err)
	}
	err := validateString(t, "m = 1", conf{})
	if err == nil || !strings.Contains(err.Error(), "expected an object") {
		t.Errorf("got %v", err)
	}
	err = validateString(t, "m { }", conf{})
	if err == nil || !strings.Contains(err.Error(), "at least 1 items") {
		t.Errorf("got %v", err)
	}
}

func TestValidateNestedPointerAndSliceOfStructs(t *testing.T) {
	type server struct {
		Host string `ucl:"host,required"`
		Port int    `ucl:"port,max=65535"`
	}
	type conf struct {
		Primary *server  `ucl:"primary"`
		Extra   []server `ucl:"extra"`
	}
	if err := validateString(t, "primary { host = a }\nextra = [{host = b}]", conf{}); err != nil {
		t.Errorf("a valid document was rejected: %v", err)
	}
	err := validateString(t, "primary { port = 70000 }\nextra = [{host = b}, {port = 1}]", conf{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		"primary.host: is required",
		"primary.port: must be at most 65535",
		"extra[1].host: is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

// A mistyped constraint must be reported rather than silently ignored: an
// option that does nothing means validation that does nothing.
func TestStructTagOptionErrors(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{"value on a flag", `ucl:"a,required=1"`, `option "required" takes no value`},
		{"value on unique", `ucl:"a,unique=yes"`, `option "unique" takes no value`},
		{"missing value", `ucl:"a,min"`, `option "min" needs a value`},
		{"empty value", `ucl:"a,max="`, `option "max" needs a value`},
		{"bad pattern", `ucl:"a,pattern=([ "`, "invalid pattern"},
		{"unknown option", `ucl:"a,bogus"`, `unknown option "bogus"`},
		{"required with default", `ucl:"a,required,default=1"`, "required and default cannot both be given"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "A",
				Type: reflect.TypeOf(0),
				Tag:  reflect.StructTag(tc.tag),
			}})
			err := Validate(mustParse(t, "a = 1"), reflect.New(typ).Elem().Interface())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The same unknown option inside a json tag has to stay silent, because it may
// be meant for encoding/json.
func TestJSONTagOptionsAreIgnored(t *testing.T) {
	type conf struct {
		A int `json:"a,string,omitempty"`
		B int `json:"b,bogus"`
	}
	if err := validateString(t, "a = 1\nb = 2", conf{}); err != nil {
		t.Errorf("json tag options should be ignored: %v", err)
	}
	var c conf
	if err := Unmarshal([]byte("a = 1\nb = 2"), &c); err != nil {
		t.Fatal(err)
	}
	if c.A != 1 || c.B != 2 {
		t.Errorf("got %+v", c)
	}
}

// len is an exact size, measured the way min and max measure.
func TestValidateLen(t *testing.T) {
	type conf struct {
		Code  string   `ucl:"code,len=3"`
		Pair  []int    `ucl:"pair,len=2"`
		Exact int      `ucl:"exact,len=7"`
		Tags  []string `ucl:"tags,len=1"`
	}
	if err := validateString(t, "code = abc\npair = [1, 2]\nexact = 7\ntags = one", conf{}); err != nil {
		t.Errorf("a valid document was rejected: %v", err)
	}
	err := validateString(t, "code = ab\npair = [1]\nexact = 8", conf{})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		"code: must be exactly 3 characters, got 2 characters",
		"pair: must be exactly 2 items, got 1 items",
		"exact: must be exactly 7, got 8",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}
