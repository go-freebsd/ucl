package ucl_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// A document can pull in others. Relative names are resolved against the
// directory of the including file.
func ExampleParse_include() {
	obj, err := ucl.ParseString(`
		local = yes
		.include "../../testdata/defaults.conf"
	`)
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("local").Bool())
	fmt.Println(obj.Get("mode").Str())
	fmt.Println(obj.Get("timeout").Duration())
	// Output:
	// true
	// safe
	// 10s
}

// An optional set of drop in files is the usual conf.d arrangement: glob
// expands the pattern and try tolerates it matching nothing.
func ExampleParse_tryInclude() {
	obj, err := ucl.ParseString(`
		.include(glob = true, try = true) "../../testdata/conf.d/*.conf"
		.include(try = true) "../../testdata/not-installed.conf"
	`)
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("first").Int(), obj.Get("second").Int())
	fmt.Println(obj.Has("missing"))
	// Output:
	// 1 2
	// false
}

// Without try, a missing file stops the parse with ErrNotFound.
func ExampleErrNotFound() {
	_, err := ucl.ParseString(`.include "../../testdata/not-installed.conf"`)

	fmt.Println(errors.Is(err, ucl.ErrNotFound))
	fmt.Println(err)
	// Output:
	// true
	// ucl: included file not found: ../../testdata/not-installed.conf
}

// Includes are also looked for in the directories given to the parser, which
// is how a program offers a search path of its own.
func ExampleWithIncludePath() {
	obj, err := ucl.ParseString(`.include "snippet.conf"`,
		ucl.WithIncludePath("../../testdata"))
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Get("shared_secret").Str())
	// Output: s3cret
}

// A macro of your own receives the argument, the parameters and the object it
// was invoked in.
func ExampleWithMacro() {
	shout := func(ctx *ucl.MacroContext) error {
		key := ctx.Params.Get("key").StrOr("shout")
		value := strings.ToUpper(ctx.Value.ToString())
		return ctx.Target.Insert(key, ucl.NewString(value), ucl.DuplicateRewrite)
	}

	obj, err := ucl.ParseString(`
		.shout(key = greeting) "hello"
		section {
			.shout "quietly"
		}
	`, ucl.WithMacro("shout", shout))
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("greeting").Str())
	fmt.Println(obj.Lookup("section.shout").Str())
	// Output:
	// HELLO
	// QUIETLY
}

// The .load macro reads a file and stores its contents as a value.
func ExampleParse_load() {
	obj, err := ucl.ParseString(`.load(key = motd) "../../testdata/motd.txt"`)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%q\n", obj.Get("motd").Str())
	// Output: "Welcome to this machine."
}
