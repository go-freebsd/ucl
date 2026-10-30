package ucl_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Code blocks in README.md may be marked with an HTML comment naming the
// Example they are copied from:
//
//	<!-- example: ExampleParseString -->
//	```go
//	...
//	```
//
// A plain block may likewise be marked "<!-- output: ExampleParseString -->"
// and is then compared against that Example's Output comment.
//
// TestREADMEMatchesExamples checks that each marked block is byte for byte the
// body, or the expected output, of that Example, so a README snippet cannot
// drift away from code that is known to compile and produce what is shown.
// Blocks without a marker are not checked; they are illustrations that cannot
// be run, such as the ones that read /usr/local/etc/pkg.conf.
func TestREADMEMatchesExamples(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	bodies := exampleBodies(t)

	blocks := markedBlocks(string(readme))
	if len(blocks) == 0 {
		t.Fatal("README.md contains no marked example blocks")
	}
	for key, block := range blocks {
		kind, name, _ := strings.Cut(key, " ")
		ex, ok := bodies[name]
		if !ok {
			t.Errorf("README.md refers to %s, which no example file defines", name)
			continue
		}
		want := ex.body
		if kind == "output" {
			want = ex.output
		}
		if block != want {
			t.Errorf("README.md %s block for %s has drifted.\n--- README ---\n%s\n--- %s ---\n%s",
				kind, name, block, name, want)
		}
	}
}

var markerRE = regexp.MustCompile("(?m)^<!-- (example|output): (Example[A-Za-z0-9_]*) -->\n```(?:go)?\n")

// markedBlocks returns the contents of every marked code block, keyed by the
// marker kind and the example name.
func markedBlocks(readme string) map[string]string {
	out := make(map[string]string)
	for _, m := range markerRE.FindAllStringSubmatchIndex(readme, -1) {
		kind := readme[m[2]:m[3]]
		name := readme[m[4]:m[5]]
		rest := readme[m[1]:]
		end := strings.Index(rest, "\n```")
		if end < 0 {
			continue
		}
		out[kind+" "+name] = rest[:end]
	}
	return out
}

// example holds the two parts of an Example that a README may quote.
type example struct {
	body   string
	output string
}

// exampleBodies returns the body and the expected output of every Example
// function, with the enclosing braces and one level of indentation removed.
func exampleBodies(t *testing.T) map[string]example {
	t.Helper()

	files, err := filepath.Glob("example*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]example)
	fset := token.NewFileSet()
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Example") {
				continue
			}
			lo := fset.Position(fn.Body.Lbrace).Offset + 1
			hi := fset.Position(fn.Body.Rbrace).Offset
			raw := string(src[lo:hi])
			out[fn.Name.Name] = example{body: dedent(raw), output: wantOutput(raw)}
		}
	}
	return out
}

// wantOutput returns the text of the Output comment that closes an Example.
func wantOutput(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "// Output:") {
			lines = lines[i+1:]
			for j, l := range lines {
				l = strings.TrimPrefix(strings.TrimSpace(l), "//")
				lines[j] = strings.TrimPrefix(l, " ")
			}
			return strings.Trim(strings.Join(lines, "\n"), "\n")
		}
	}
	return ""
}

// dedent removes the trailing Output comment, strips one tab from every line
// and trims the blank lines at either end.
func dedent(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "// Output:") {
			lines = lines[:i]
			break
		}
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "\t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}
