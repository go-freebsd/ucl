package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

const doc = `name = web
workers = 4
timeout = 30s
hosts = [alpha, beta]
database { host = db.example.net, port = 5432 }
`

// runString feeds src on standard input and returns what the command wrote.
func runString(t *testing.T, src, format, path string, options ...ucl.Option) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(strings.NewReader(src), &out, "-", format, path, options)
	return out.String(), err
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunFormats(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"ucl", `"name" = "web";`},
		{"config", `"name" = "web";`},
		{"json", "{\n    \"name\": \"web\","},
		{"json-compact", `{"name":"web","workers":4`},
		{"compact", `{"name":"web"`},
		{"yaml", "name: web\nworkers: 4"},
		{"YAML", "name: web"},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			got, err := runString(t, doc, tc.format, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("got:\n%s\nwant it to contain:\n%s", got, tc.want)
			}
		})
	}
}

func TestRunMsgPackRoundTrips(t *testing.T) {
	var out bytes.Buffer
	if err := run(strings.NewReader(doc), &out, "-", "msgpack", "", nil); err != nil {
		t.Fatal(err)
	}
	obj, err := ucl.ParseMsgPack(out.Bytes())
	if err != nil {
		t.Fatalf("the msgpack output does not parse: %v", err)
	}
	if obj.Get("name").Str() != "web" {
		t.Errorf("got %s", obj.ToString())
	}
}

func TestRunUnknownFormat(t *testing.T) {
	_, err := runString(t, doc, "toml", "")
	if err == nil || !strings.Contains(err.Error(), "unknown output format") {
		t.Errorf("got %v", err)
	}
}

// A path that selects a single value prints it plainly, so that a shell can
// use the result without stripping quotes.
func TestRunScalarIsRaw(t *testing.T) {
	tests := []struct{ path, want string }{
		{"name", "web\n"},
		{"workers", "4\n"},
		{"hosts.1", "beta\n"},
		{"database.host", "db.example.net\n"},
		{"database.port", "5432\n"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got, err := runString(t, doc, "ucl", tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Asking for another format prints the value the way that format renders it.
func TestRunScalarInOtherFormats(t *testing.T) {
	got, err := runString(t, doc, "json", "name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "\"web\"\n" {
		t.Errorf("got %q", got)
	}
}

func TestRunContainerPath(t *testing.T) {
	got, err := runString(t, doc, "json-compact", "database")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"host":"db.example.net","port":5432}` {
		t.Errorf("got %q", got)
	}
}

func TestRunMissingPath(t *testing.T) {
	_, err := runString(t, doc, "ucl", "database.nope")
	if err == nil || !strings.Contains(err.Error(), "no such key: database.nope") {
		t.Errorf("got %v", err)
	}
}

func TestRunFromFile(t *testing.T) {
	path := write(t, t.TempDir(), "app.conf", doc)
	var out bytes.Buffer
	if err := run(nil, &out, path, "ucl", "name", nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "web\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestRunMissingFile(t *testing.T) {
	err := run(nil, &bytes.Buffer{}, filepath.Join(t.TempDir(), "nope.conf"), "ucl", "", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestRunSyntaxErrorNamesStdin(t *testing.T) {
	_, err := runString(t, "a = [1, 2\n", "ucl", "")
	if err == nil || !strings.Contains(err.Error(), "<stdin>:") {
		t.Errorf("got %v", err)
	}
}

// Includes are resolved while parsing, so the output is the document the
// parser actually sees.
func TestRunResolvesIncludesAndVariables(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "defaults.conf", "workers = 1\nmode = safe\n")

	options, err := opts(0, "append", false, false, stringList{"ABI=FreeBSD:14:amd64"}, stringList{dir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := runString(t, ".include \"defaults.conf\"\nabi = \"${ABI}\"\n", "json-compact", "", options...)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"workers":1,"mode":"safe","abi":"FreeBSD:14:amd64"}` {
		t.Errorf("got %s", got)
	}
}

func TestOptsDuplicateStrategies(t *testing.T) {
	src := "s { a = 1 }\ns { b = 2 }\n"
	tests := []struct{ strategy, want string }{
		{"append", `{"s":[{"a":1},{"b":2}]}`},
		{"merge", `{"s":{"a":1,"b":2}}`},
		{"MERGE", `{"s":{"a":1,"b":2}}`},
		{"rewrite", `{"s":{"b":2}}`},
	}
	for _, tc := range tests {
		t.Run(tc.strategy, func(t *testing.T) {
			options, err := opts(0, tc.strategy, false, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := runString(t, src, "json-compact", "", options...)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}

	options, err := opts(0, "error", false, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runString(t, src, "ucl", "", options...); err == nil {
		t.Error("the error strategy did not report the duplicate")
	}
}

func TestOptsRejectsBadSettings(t *testing.T) {
	if _, err := opts(0, "nope", false, false, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "unknown duplicate strategy") {
		t.Errorf("got %v", err)
	}
	if _, err := opts(99, "append", false, false, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Errorf("got %v", err)
	}
	for _, bad := range []string{"novalue", "=novalue"} {
		if _, err := opts(0, "append", false, false, stringList{bad}, nil); err == nil ||
			!strings.Contains(err.Error(), "bad variable") {
			t.Errorf("%q: got %v", bad, err)
		}
	}
}

func TestOptsLowercaseAndNoMacros(t *testing.T) {
	options, err := opts(0, "append", true, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := runString(t, "ABI = x\nSection { Sub = 1 }", "json-compact", "", options...)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"abi":"x","section":{"sub":1}}` {
		t.Errorf("got %s", got)
	}

	// Without -M an unknown macro is an error; with it the dot is a key.
	if _, err := runString(t, `.include "nope.conf"`, "ucl", ""); err == nil {
		t.Error("the include should have failed")
	}
	options, err = opts(0, "append", false, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err = runString(t, ".include = 1", "json-compact", "", options...)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{".include":1}` {
		t.Errorf("got %s", got)
	}
}

func TestStringListFlag(t *testing.T) {
	var s stringList
	if err := s.Set("a=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("b=2"); err != nil {
		t.Fatal(err)
	}
	if got := s.String(); got != "a=1,b=2" {
		t.Errorf("got %q", got)
	}
}
