package ucl_test

import (
	"fmt"
	"time"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

// The usual shape of a program: parse a document, check it against the
// constraints in the struct tags, then decode it.
func Example() {
	type Repository struct {
		URL     string `ucl:"url,required"`
		Enabled bool   `ucl:"enabled"`
	}
	type Config struct {
		ABI     string        `ucl:"ABI,required,pattern=^FreeBSD:[0-9]+:"`
		Repos   []string      `ucl:"REPOS_DIR,min=1,unique"`
		Timeout time.Duration `ucl:"FETCH_TIMEOUT,min=1s,max=10min"`
		FreeBSD Repository    `ucl:"FreeBSD"`
	}

	// Defaults first; only the keys the document mentions are replaced.
	cfg := Config{Timeout: 5 * time.Second}

	obj, err := ucl.ParseFile("../../testdata/pkg.conf")
	if err != nil {
		panic(err)
	}
	if err := ucl.Validate(obj, cfg); err != nil {
		panic(err)
	}
	if err := obj.Decode(&cfg); err != nil {
		panic(err)
	}

	fmt.Println(cfg.ABI)
	fmt.Println(cfg.Repos)
	fmt.Println(cfg.Timeout)
	fmt.Println(cfg.FreeBSD.Enabled)
	// Output:
	// FreeBSD:14:amd64
	// [/etc/pkg /usr/local/etc/pkg/repos]
	// 30s
	// true
}

// UCL is a strict superset of JSON, so a JSON document parses unchanged.
func ExampleParse() {
	obj, err := ucl.Parse([]byte(`{"server": {"ports": [80, 443]}}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Lookup("server.ports.1").Int())
	// Output: 443
}

func ExampleParseString() {
	obj, err := ucl.ParseString(`
		ABI = "FreeBSD:14:amd64";
		REPOS_DIR [ "/etc/pkg", "/usr/local/etc/pkg/repos" ]
		FreeBSD {
			url = "pkg+https://pkg.FreeBSD.org/${ABI}/quarterly";
			enabled = yes;
		}
		FETCH_TIMEOUT = 30s;
	`, ucl.WithVariable("ABI", "FreeBSD:14:amd64"))
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("ABI").Str())
	fmt.Println(obj.Lookup("REPOS_DIR.1").Str())
	fmt.Println(obj.Lookup("FreeBSD.url").Str())
	fmt.Println(obj.Get("FETCH_TIMEOUT").Duration())
	// Output:
	// FreeBSD:14:amd64
	// /usr/local/etc/pkg/repos
	// pkg+https://pkg.FreeBSD.org/FreeBSD:14:amd64/quarterly
	// 30s
}

// A tour of the syntax: three ways to separate a key from its value, nested
// sections without an equals sign, comments, raw single quoted strings and
// heredocs.
func ExampleParseString_syntax() {
	obj, err := ucl.ParseString(`
		equals  = one          # a comment runs to the end of the line
		colon:    two
		none      three        /* block comments nest */

		section {
			spaces = an unquoted value may contain spaces
			raw    = 'no \n escapes and no $expansion'
			banner = <<EOD
first line
second line
EOD
		}
	`)
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("equals").Str())
	fmt.Println(obj.Get("colon").Str())
	fmt.Println(obj.Get("none").Str())
	fmt.Println(obj.Lookup("section.spaces").Str())
	fmt.Println(obj.Lookup("section.raw").Str())
	fmt.Println(obj.Lookup("section.banner").Str())
	// Output:
	// one
	// two
	// three
	// an unquoted value may contain spaces
	// no \n escapes and no $expansion
	// first line
	// second line
}

// Numbers may carry a size or a time suffix, and booleans have six spellings.
func ExampleParseString_numbers() {
	obj, err := ucl.ParseString(`
		decimal = 1k        # powers of 1000
		binary  = 1kb       # powers of 1024
		memory  = 512mb
		wait    = 30s
		short   = 500ms
		enabled = yes
		version = 1.2.3     # not a number, so it stays a string
	`)
	if err != nil {
		panic(err)
	}

	fmt.Println(obj.Get("decimal").Int(), obj.Get("binary").Int())
	fmt.Println(obj.Get("memory").Int())
	fmt.Println(obj.Get("wait").Duration(), obj.Get("short").Duration())
	fmt.Println(obj.Get("enabled").Bool())
	fmt.Println(obj.Get("version").Type(), obj.Get("version").Str())
	// Output:
	// 1000 1024
	// 536870912
	// 30s 500ms
	// true
	// string 1.2.3
}

func ExampleParseFile() {
	obj, err := ucl.ParseFile("../../testdata/pkg.conf")
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Get("ABI").Str())
	fmt.Println(obj.Lookup("FreeBSD.mirror_type").Str())
	// Output:
	// FreeBSD:14:amd64
	// srv
}
