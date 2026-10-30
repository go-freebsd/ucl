package ucl_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// A parser accepts several documents and merges them into one result, which
// is how layered configuration is expressed: shipped defaults first, then
// whatever the operator changed.
func ExampleParser() {
	p := ucl.NewParser(ucl.WithDuplicateStrategy(ucl.DuplicateRewrite))
	if err := p.AddFile("../../testdata/defaults.conf"); err != nil {
		panic(err)
	}
	if err := p.AddString("workers = 8"); err != nil {
		panic(err)
	}

	cfg := p.Result()
	fmt.Println(cfg.Get("workers").Int())
	fmt.Println(cfg.Get("mode").Str())
	fmt.Println(cfg.Get("timeout").Duration())
	// Output:
	// 8
	// safe
	// 10s
}

// The name given to AddReader appears in error messages and sets the
// directory that relative includes are resolved against.
func ExampleParser_AddReader() {
	p := ucl.NewParser()
	err := p.AddReader(strings.NewReader("a = 1\nb = \"unterminated\n"), "site.conf")
	fmt.Println(err)
	// Output:
	// site.conf:2:5: unterminated string
}

// A parse failure carries the position of the problem. File is empty for a
// document that did not come from a file, and the formatted message says
// "<input>" instead.
func ExampleSyntaxError() {
	_, err := ucl.ParseString("name = web\nbroken = 'oops\n")
	fmt.Println(err)

	var se *ucl.SyntaxError
	if errors.As(err, &se) {
		fmt.Printf("line %d, column %d: %s\n", se.Line, se.Column, se.Msg)
	}
	// Output:
	// <input>:2:10: unterminated string
	// line 2, column 10: unterminated string
}

// Variables are expanded in unquoted and double quoted strings. A reference
// that cannot be resolved is left alone.
func ExampleWithVariable() {
	obj, err := ucl.ParseString(`
		bindir  = "${PREFIX}/bin"
		etcdir  = $PREFIX/etc
		unknown = "${NOPE}"
	`, ucl.WithVariable("PREFIX", "/usr/local"))
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("bindir").Str())
	fmt.Println(obj.Get("etcdir").Str())
	fmt.Println(obj.Get("unknown").Str())
	// Output:
	// /usr/local/bin
	// /usr/local/etc
	// ${NOPE}
}

// A handler resolves variables that were not registered up front. Returning
// false leaves the reference in place.
func ExampleWithVariableHandler() {
	obj, err := ucl.ParseString(`a = "${HOST}"`+"\n"+`b = "${OTHER}"`,
		ucl.WithVariableHandler(func(name string) (string, bool) {
			if name == "HOST" {
				return "example.net", true
			}
			return "", false
		}))
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("a").Str())
	fmt.Println(obj.Get("b").Str())
	// Output:
	// example.net
	// ${OTHER}
}

// By default a repeated section becomes an array of sections. Merging folds
// them into one instead.
func ExampleWithDuplicateStrategy() {
	const src = "server { host = alpha }\nserver { port = 8080 }\n"

	appended, err := ucl.ParseString(src)
	if err != nil {
		panic(err)
	}
	merged, err := ucl.ParseString(src, ucl.WithDuplicateStrategy(ucl.DuplicateMerge))
	if err != nil {
		panic(err)
	}

	fmt.Println(appended.Get("server").ToString())
	fmt.Println(merged.Get("server").ToString())
	// Output:
	// [{"host":"alpha"},{"port":8080}]
	// {"host":"alpha","port":8080}
}

// A value of a higher priority wins when a key repeats, whichever document it
// came from. A package may therefore override a system default regardless of
// the order the two are read in.
func ExampleWithPriority() {
	system, err := ucl.ParseString("workers = 1\nmode = safe", ucl.WithPriority(1))
	if err != nil {
		panic(err)
	}
	pkg, err := ucl.ParseString("workers = 8", ucl.WithPriority(5))
	if err != nil {
		panic(err)
	}

	if err := system.Merge(pkg, ucl.DuplicateAppend); err != nil {
		panic(err)
	}
	fmt.Println(system.Get("workers").Int())

	// The same merge the other way round keeps the higher priority value.
	low, err := ucl.ParseString("workers = 1", ucl.WithPriority(1))
	if err != nil {
		panic(err)
	}
	if err := pkg.Merge(low, ucl.DuplicateAppend); err != nil {
		panic(err)
	}
	fmt.Println(pkg.Get("workers").Int())
	// Output:
	// 8
	// 8
}

// Configuration files that are inconsistent about capitalisation can be
// folded to lower case while parsing.
func ExampleWithLowercaseKeys() {
	obj, err := ucl.ParseString("ABI = x\nSection { Sub = 1 }", ucl.WithLowercaseKeys())
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Keys())
	fmt.Println(obj.Lookup("section.sub").Int())
	// Output:
	// [abi section]
	// 1
}

// Without implicit arrays a repeated key simply overwrites, the way most
// non UCL configuration formats behave.
func ExampleWithoutImplicitArrays() {
	obj, err := ucl.ParseString("listen = a\nlisten = b", ucl.WithoutImplicitArrays())
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Get("listen").ToString())
	// Output: b
}

// Time suffixes can be switched off for documents where a value such as 10s
// is meant to stay a string.
func ExampleWithoutTime() {
	obj, err := ucl.ParseString("version = 10s", ucl.WithoutTime())
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Get("version").Type(), obj.Get("version").Str())
	// Output: string 10s
}
