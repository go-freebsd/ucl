package ucl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestInclude(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "included.conf", "b = 2\nc = 3\n")
	main := write(t, dir, "main.conf", "a = 1\n.include \"included.conf\"\nd = 4\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"a":1,"b":2,"c":3,"d":4}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludeIntoSection(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "x = 1\n")
	main := write(t, dir, "main.conf", "s {\n.include \"sub.conf\"\n}\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"s":{"x":1}}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	main := write(t, dir, "main.conf", ".include \"nope.conf\"\n")
	_, err := ParseFile(main)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestIncludeTry(t *testing.T) {
	dir := t.TempDir()
	main := write(t, dir, "main.conf", "a = 1\n.include(try=true) \"nope.conf\"\n")
	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"a":1}` {
		t.Errorf("got %s", got)
	}
}

func TestTryInclude(t *testing.T) {
	dir := t.TempDir()
	main := write(t, dir, "main.conf", "a = 1\n.try_include \"nope.conf\"\n")
	if _, err := ParseFile(main); err != nil {
		t.Fatal(err)
	}
}

func TestIncludeGlob(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "conf.d/10-a.conf", "a = 1\n")
	write(t, dir, "conf.d/20-b.conf", "b = 2\n")
	main := write(t, dir, "main.conf", `.include(glob=true) "conf.d/*.conf"`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"a":1,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludeDuplicateMerge(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "s { b = 2 }\n")
	main := write(t, dir, "main.conf", "s { a = 1 }\n.include(duplicate=merge) \"sub.conf\"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"s":{"a":1,"b":2}}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludePriority(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "a = 2\n")
	main := write(t, dir, "main.conf", "a = 1\n.include(priority=3) \"sub.conf\"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Get("a").Int(); got != 2 {
		t.Errorf("got %d, want the value of the higher priority include", got)
	}
}

func TestIncludePrefix(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "db.conf", "host = localhost\n")
	main := write(t, dir, "main.conf", `.include(prefix=true) "db.conf"`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"db":{"host":"localhost"}}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludePath(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	write(t, other, "shared.conf", "shared = true\n")
	main := write(t, dir, "main.conf", `.include "shared.conf"`+"\n")

	o, err := ParseFile(main, WithIncludePath(other))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Get("shared").Bool() {
		t.Errorf("got %s", mustJSON(t, o))
	}
}

func TestIncludeLoop(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.conf", `.include "b.conf"`+"\n")
	write(t, dir, "b.conf", `.include "a.conf"`+"\n")

	if _, err := ParseFile(filepath.Join(dir, "a.conf")); err == nil {
		t.Fatal("expected a loop error")
	}
}

func TestIncludeFileVariables(t *testing.T) {
	dir := t.TempDir()
	main := write(t, dir, "main.conf", `dir = "${CURDIR}"`+"\n")
	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(dir)
	if got := o.Get("dir").Str(); got != want {
		t.Errorf("CURDIR is %q, want %q", got, want)
	}
}

func TestSignedIncludeRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "a = 1\n")
	main := write(t, dir, "main.conf", `.includes "sub.conf"`+"\n")
	if _, err := ParseFile(main); err == nil {
		t.Fatal("expected .includes to be rejected")
	}
}

func TestLoadMacro(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "secret.txt", "hunter2\n")
	main := write(t, dir, "main.conf", `.load(key=password) "secret.txt"`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Get("password").Str(); got != "hunter2" {
		t.Errorf("got %q", got)
	}
}

func TestLoadMacroBase64(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "blob.bin", "ab")
	main := write(t, dir, "main.conf", `.load(key=blob, encode=base64) "blob.bin"`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Get("blob").Str(); got != "YWI=" {
		t.Errorf("got %q", got)
	}
}

func TestLoadMacroVariants(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "secret.txt", "hunter2\n")
	write(t, dir, "count.txt", "42\n")
	write(t, dir, "words.txt", "not a number\n")

	tests := []struct {
		name  string
		macro string
		key   string
		want  string
	}{
		{"default key from the file name", `.load "secret.txt"`, "secret", "hunter2"},
		{"explicit key", `.load(key=password) "secret.txt"`, "password", "hunter2"},
		{"hex", `.load(key=h, encode=hex) "count.txt"`, "h", "34320a"},
		{"base64", `.load(key=b, encode=base64) "count.txt"`, "b", "NDIK"},
		{"explicit none", `.load(key=n, encode=none) "secret.txt"`, "n", "hunter2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			main := write(t, dir, "main.conf", tc.macro+"\n")
			o, err := ParseFile(main)
			if err != nil {
				t.Fatal(err)
			}
			if got := o.Get(tc.key).Str(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	// type=int turns the contents into a number rather than a string.
	main := write(t, dir, "main.conf", `.load(key=n, type=int) "count.txt"`+"\n")
	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if v := o.Get("n"); v.Type() != TypeInt || v.Int() != 42 {
		t.Errorf("got %s %s", v.Type(), v.ToString())
	}
}

func TestLoadMacroErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "words.txt", "not a number\n")

	tests := []struct{ name, macro, want string }{
		{"not an integer", `.load(key=n, type=int) "words.txt"`, "as an integer"},
		{"unknown encoding", `.load(key=n, encode=rot13) "words.txt"`, "unknown .load encoding"},
		{"missing file", `.load(key=n) "absent.txt"`, "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			main := write(t, dir, "main.conf", tc.macro+"\n")
			_, err := ParseFile(main)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}

	// try turns a missing file into silence.
	main := write(t, dir, "main.conf", "a = 1\n"+`.load(key=n, try=true) "absent.txt"`+"\n")
	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if o.Has("n") {
		t.Error("a missing file should leave no key behind")
	}
}

// The duplicate parameter accepts the same four names as the parser option.
func TestIncludeDuplicateParameter(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "s { b = 2 }\n")

	tests := []struct{ strategy, want string }{
		{"append", `{"s":[{"a":1},{"b":2}]}`},
		{"merge", `{"s":{"a":1,"b":2}}`},
		{"rewrite", `{"s":{"b":2}}`},
	}
	for _, tc := range tests {
		t.Run(tc.strategy, func(t *testing.T) {
			main := write(t, dir, "main.conf",
				"s { a = 1 }\n.include(duplicate="+tc.strategy+") \"sub.conf\"\n")
			o, err := ParseFile(main)
			if err != nil {
				t.Fatal(err)
			}
			if got := mustJSON(t, o); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}

	main := write(t, dir, "main.conf", "s { a = 1 }\n.include(duplicate=error) \"sub.conf\"\n")
	if _, err := ParseFile(main); err == nil {
		t.Error("the error strategy should report the duplicate")
	}

	main = write(t, dir, "main.conf", `.include(duplicate=nonsense) "sub.conf"`+"\n")
	_, err := ParseFile(main)
	if err == nil || !strings.Contains(err.Error(), "unknown duplicate strategy") {
		t.Errorf("got %v", err)
	}
}

// A list of files can be included in one macro.
func TestIncludeListOfFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.conf", "a = 1\n")
	write(t, dir, "two.conf", "b = 2\n")
	main := write(t, dir, "main.conf", `.include ["one.conf", "two.conf"]`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, o); got != `{"a":1,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestIncludeParameterErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub.conf", "a = 1\n")

	tests := []struct{ name, macro, want string }{
		{"priority out of range", `.include(priority=99) "sub.conf"`, "invalid include priority"},
		{"url", `.include(url=true) "sub.conf"`, "not supported"},
		{"sign", `.include(sign=true) "sub.conf"`, "not supported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			main := write(t, dir, "main.conf", tc.macro+"\n")
			_, err := ParseFile(main)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The path parameter adds directories for this include only.
func TestIncludePathParameter(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	write(t, other, "shared.conf", "shared = yes\n")
	main := write(t, dir, "main.conf", `.include(path="`+other+`") "shared.conf"`+"\n")

	o, err := ParseFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Get("shared").Bool() {
		t.Errorf("got %s", mustJSON(t, o))
	}
}

func TestPriorityMacroErrors(t *testing.T) {
	for _, src := range []string{".priority 99\na = 1", ".priority nonsense\na = 1"} {
		if _, err := ParseString(src); err == nil {
			t.Errorf("%q should be rejected", src)
		}
	}
}
