// Command ucl reads a UCL or JSON document and writes it out in any of the
// formats the library supports. It is a small convenience tool, not a
// replacement for FreeBSD's uclcmd(1): it never writes a document back.
//
// Usage:
//
//	ucl [flags] [path]
//
// The optional path selects a part of the document, using dots to separate
// the components, for example "FreeBSD.url" or "REPOS_DIR.0". When a path
// selects a single value and the output format is the default UCL syntax, the
// value is printed on its own without quotes, so that it can be used directly
// in a shell:
//
//	url=$(ucl -f /usr/local/etc/pkg.conf FreeBSD.url)
//
// Asking for another format prints the value as that format renders it, so
// "ucl -o json -f pkg.conf FreeBSD.url" writes a quoted JSON string.
//
// Because includes are resolved and variables expanded while parsing, running
// the tool over a configuration file shows what the parser actually sees:
//
//	ucl -f /usr/local/etc/pkg.conf
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-freebsd/ucl/pkg/ucl"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var (
		file      = flag.String("f", "-", "read the document from `file`, - means standard input")
		format    = flag.String("o", "ucl", "output `format`: ucl, json, json-compact, yaml or msgpack")
		priority  = flag.Uint("p", 0, "parse the document with `priority` 0 to 15")
		duplicate = flag.String("d", "append", "duplicate key `strategy`: append, merge, rewrite or error")
		lowercase = flag.Bool("l", false, "convert all keys to lower case")
		noMacros  = flag.Bool("M", false, "disable macros such as .include")
		vars      stringList
		paths     stringList
	)
	flag.Var(&vars, "D", "define a variable as `name=value`, repeatable")
	flag.Var(&paths, "I", "add `dir` to the include search path, repeatable")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() > 1 {
		usage()
		os.Exit(2)
	}

	options, err := opts(*priority, *duplicate, *lowercase, *noMacros, vars, paths)
	if err == nil {
		err = run(os.Stdin, os.Stdout, *file, *format, flag.Arg(0), options)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ucl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: ucl [flags] [path]\n\n")
	flag.PrintDefaults()
}

// opts turns the command line settings into parser options.
func opts(priority uint, duplicate string, lowercase, noMacros bool, vars, paths stringList) ([]ucl.Option, error) {
	if priority > ucl.MaxPriority {
		return nil, fmt.Errorf("priority %d is out of range, it must be 0 to %d", priority, ucl.MaxPriority)
	}
	out := []ucl.Option{ucl.WithPriority(uint8(priority))}

	switch strings.ToLower(duplicate) {
	case "append":
		// The default.
	case "merge":
		out = append(out, ucl.WithDuplicateStrategy(ucl.DuplicateMerge))
	case "rewrite":
		out = append(out, ucl.WithDuplicateStrategy(ucl.DuplicateRewrite))
	case "error":
		out = append(out, ucl.WithDuplicateStrategy(ucl.DuplicateError))
	default:
		return nil, fmt.Errorf("unknown duplicate strategy %q, it must be append, merge, rewrite or error", duplicate)
	}

	if lowercase {
		out = append(out, ucl.WithLowercaseKeys())
	}
	if noMacros {
		out = append(out, ucl.WithoutMacros())
	}
	for _, v := range vars {
		name, value, ok := strings.Cut(v, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("bad variable %q, it must be written name=value", v)
		}
		out = append(out, ucl.WithVariable(name, value))
	}
	if len(paths) > 0 {
		out = append(out, ucl.WithIncludePath(paths...))
	}
	return out, nil
}

// run parses one document and writes the requested part of it to w.
func run(in io.Reader, w io.Writer, file, format, path string, options []ucl.Option) error {
	f, err := parseFormat(format)
	if err != nil {
		return err
	}

	p := ucl.NewParser(options...)
	if file == "-" {
		err = p.AddReader(in, "<stdin>")
	} else {
		err = p.AddFile(file)
	}
	if err != nil {
		return err
	}

	obj := p.Result()
	if path != "" {
		if obj = obj.Lookup(path); obj == nil {
			return fmt.Errorf("no such key: %s", path)
		}
	}

	// A single value is not a document, so in configuration syntax it is
	// written plainly rather than as a quoted literal.
	if f == ucl.Config && !obj.IsContainer() {
		_, err := fmt.Fprintln(w, obj.ToString())
		return err
	}
	return obj.EmitTo(w, f)
}

func parseFormat(s string) (ucl.Format, error) {
	switch strings.ToLower(s) {
	case "ucl", "config":
		return ucl.Config, nil
	case "json":
		return ucl.JSON, nil
	case "json-compact", "compact":
		return ucl.JSONCompact, nil
	case "yaml", "yml":
		return ucl.YAML, nil
	case "msgpack":
		return ucl.MsgPack, nil
	}
	return 0, fmt.Errorf("unknown output format %q", s)
}
