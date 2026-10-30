# ucl

[![Go Reference](https://pkg.go.dev/badge/github.com/go-freebsd/ucl/pkg/ucl.svg)](https://pkg.go.dev/github.com/go-freebsd/ucl/pkg/ucl)

A pure Go implementation of the **Universal Configuration Language**, the
configuration format used throughout FreeBSD: `pkg.conf`, pkg repository
definitions, package manifests, `uclcmd(1)` and most of `/usr/local/etc`.

It follows [libucl](https://github.com/vstakhov/libucl), the reference C
implementation that ships in the FreeBSD base system, closely: the same
syntax, the same numeric suffixes, the same implicit arrays, priorities and
duplicate handling, the same `.include` macros and the same output formats.
No cgo, no dependencies outside the standard library.

```
go get github.com/go-freebsd/ucl
```

The package lives below `pkg`, so the import path is one component longer than
the module path:

```go
import "github.com/go-freebsd/ucl/pkg/ucl"
```

## Quick start

Parse a document and read values out of it. Every read accessor tolerates a
nil receiver, so a missing key needs no special case:

<!-- example: ExampleParseString -->
```go
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
```

Decoding straight into Go values works like `encoding/json`, using `ucl` tags
and falling back to `json` tags. Fields the document does not mention keep the
value they already had, so a struct can be filled with defaults first:

<!-- example: ExampleUnmarshal -->
```go
var cfg struct {
	Repos   []string      `ucl:"REPOS_DIR"`
	Timeout time.Duration `ucl:"FETCH_TIMEOUT"`
	Debug   bool          `ucl:"DEBUG_LEVEL"`
}
src := []byte("REPOS_DIR = /etc/pkg\nFETCH_TIMEOUT = 2min\nDEBUG_LEVEL = off\n")
if err := ucl.Unmarshal(src, &cfg); err != nil {
	panic(err)
}
fmt.Printf("%v %v %v\n", cfg.Repos, cfg.Timeout, cfg.Debug)
```

The same tags describe what a valid document looks like, so there is no second
schema to write:

<!-- example: ExampleValidate -->
```go
type Database struct {
	Host string `ucl:"host,required,pattern=^[a-z0-9.-]+$"`
	Port int    `ucl:"port,min=1,max=65535"`
}
type Config struct {
	Workers  int           `ucl:"workers,min=1,max=64"`
	Timeout  time.Duration `ucl:"timeout,min=1s,max=5min"`
	Mode     string        `ucl:"mode,enum=fast|safe"`
	Database Database      `ucl:"database"`
}

obj, err := ucl.ParseString(`
	workers = 128
	timeout = 20min
	mode    = quick
	database { host = "DB Server" }
`)
if err != nil {
	panic(err)
}
fmt.Println(ucl.Validate(obj, Config{}))
```

Every problem is reported, not just the first, and each one names the key it
came from:

<!-- output: ExampleValidate -->
```
ucl: 4 validation errors:
	workers: must be at most 64, got 128
	timeout: must be at most 5m0s, got 20m0s
	mode: must be one of fast, safe, got "quick"
	database.host: must match ^[a-z0-9.-]+$
```

And back out again, in any of the supported formats:

<!-- example: ExampleMarshal -->
```go
type Database struct {
	Host    string        `ucl:"host"`
	Port    int           `ucl:"port"`
	Timeout time.Duration `ucl:"timeout"`
	Comment string        `ucl:"comment,omitempty"`
}

out, err := ucl.Marshal(Database{Host: "db.example.net", Port: 5432, Timeout: time.Minute}, ucl.Config)
if err != nil {
	panic(err)
}
fmt.Print(string(out))
```

The snippets above are the bodies of the runnable examples in
[the package documentation](https://pkg.go.dev/github.com/go-freebsd/ucl/pkg/ucl#pkg-examples),
and a test keeps them identical. See [Validation](#validation) for the full set
of constraint options.

## The language

```nginx
# a comment, /* block comments nest */

param = value;              # '=', ':' or nothing at all separates key and value
other: "quoted value"       # ',', ';' or a newline separates entries

section {                   # objects need no separator
    enabled = yes           # true/false, yes/no, on/off
    workers = 4
    ratio   = 0.75
    memory  = 512mb         # 536870912, 'mb' is binary, 'm' is decimal
    timeout = 30s           # a time value, in seconds
    nothing = null
    list    = [1, 2, "three"]
    raw     = 'no $expansion here'
    path    = "${CURDIR}/data"
    banner  = <<EOD
multiple
lines
EOD
}

listen = 127.0.0.1          # repeating a key builds an implicit array,
listen = ::1                # so listen becomes ["127.0.0.1", "::1"]

.include(try=true, glob=true) "conf.d/*.conf"
```

Since UCL is a strict superset of JSON, any JSON document parses too.

| Feature | Notes |
| --- | --- |
| Objects, arrays, implicit top level object | `{}`, `[]`, bare `key value` pairs |
| Comments | `#` to end of line and nested `/* */` |
| Strings | double quoted with escapes, single quoted raw, `<<EOD` heredocs |
| Numbers | ints, floats, `0x` hex, `nan`, `inf` |
| Size suffixes | `k`, `m`, `g` decimal and `kb`, `mb`, `gb` binary |
| Time suffixes | `ms`, `s`, `min`, `h`, `d`, `w`, `y`, also `ks` and `gs` |
| Booleans | `true`/`false`, `yes`/`no`, `on`/`off`, case insensitive |
| Variables | `$VAR` and `${VAR}`, plus the automatic `CURDIR` and `FILENAME` |
| Macros | `.include`, `.try_include`, `.priority`, `.load`, and your own |
| Priorities | 0 to 15, a higher priority overrides a lower one |
| Duplicate handling | append (implicit arrays), merge, rewrite, error |
| Emitters | UCL config, JSON, compact JSON, YAML, msgpack |
| Validation | constraints in Go struct tags, see below |

## Validation

Constraints are declared in the same struct tags that name the fields, so
there is no separate schema document to parse and keep in sync:

```go
type Database struct {
	Host    string `ucl:"host,required,pattern=^[a-z0-9.-]+$"`
	Port    int    `ucl:"port,min=1,max=65535"`
	User    string `ucl:"user,required"`
	SSLMode string `ucl:"sslmode,enum=disable|require|verify-full"`
}

type Config struct {
	Name     string        `ucl:"name,required,min=1,max=32"`
	Workers  int           `ucl:"workers,min=1,max=64"`
	Timeout  time.Duration `ucl:"timeout,min=1s,max=5min"`
	MaxBody  int64         `ucl:"max_body,max=64mb"`
	Mode     string        `ucl:"mode,enum=fast|safe"`
	Listen   []string      `ucl:"listen,required,min=1,unique"`
	Database Database      `ucl:"database"`
}

obj, err := ucl.ParseFile("app.conf")
if err != nil {
	return err
}
if err := ucl.Validate(obj, Config{}, ucl.RejectUnknownKeys()); err != nil {
	return err
}
if err := obj.Decode(&cfg); err != nil {
	return err
}
```

`Validate` only needs the type, so a zero value is enough. It reports every
problem rather than the first, as `ValidationErrors`, each naming the key it
came from:

```
ucl: 9 validation errors:
	name: is required
	timeout: must be at most 5m0s, got 20m0s
	max_body: must be at most 67108864, got 134217728
	listen: must not contain the duplicate value "127.0.0.1:8080"
	database.host: must match ^[a-z0-9.-]+$
	database.port: must be at most 65535, got 70000
	database.user: is required
	database.sslmode: must be one of disable, require, verify-full, got "true"
	wrokers: unknown key
```

| Option | Applies to | Meaning |
| --- | --- | --- |
| `default=` | any | the value to use when the key is absent, itself checked against the constraints below |
| `required` | any | the key must be present |
| `min=`, `max=` | numbers | inclusive bounds on the value |
| | strings | bounds on the length in runes |
| | slices, arrays, maps | bounds on the number of elements |
| `len=` | the same | an exact value, length or count |
| `unique` | slices, arrays | no repeated element |
| `pattern=` | strings | a Go regular expression |
| `enum=a\|b\|c` | scalars | the set of accepted values |
| `multipleof=` | numbers | the value must divide evenly |

The last three describe a single value, so on a slice, an array or a map they
are applied to each element and a problem names the element it was found in:
`hosts[1]` or `levels.db`. The others describe the field as a whole, so
`min` and `max` keep counting the elements of a list.

Bounds may use the suffixes a document uses, so `max=64mb` and `min=1s` work
on an integer and on a `time.Duration` field, and so does `default=30s`. A field
the caller filled in before decoding keeps that value, so `default=` and an
already populated struct compose rather than fight. Type mismatches are reported
too, which turns a decoder error into a message that names the exact key.
`RejectUnknownKeys` is the equivalent of `"additionalProperties": false` and
catches misspelled options. Fields without any constraint are not restricted,
so adding tags is incremental, and the options live in `ucl` tags only: a
`json` tag is still read for the field name but never for constraints.

## Parser options

```go
p := ucl.NewParser(
	ucl.WithVariable("PREFIX", "/usr/local"),
	ucl.WithVariableHandler(func(name string) (string, bool) { … }),
	ucl.WithIncludePath("/usr/local/etc/myapp"),
	ucl.WithDuplicateStrategy(ucl.DuplicateMerge),
	ucl.WithPriority(5),
	ucl.WithLowercaseKeys(),
	ucl.WithoutImplicitArrays(),
	ucl.WithoutTime(),
	ucl.WithoutMacros(),
	ucl.WithoutFileVariables(),
	ucl.WithMaxIncludeDepth(8),
	ucl.WithMacro("hostname", myHandler),
)
if err := p.AddFile("/usr/local/etc/myapp.conf"); err != nil {
	return err
}
obj := p.Result()
```

A parser can consume several documents; later ones are merged into the result
according to the duplicate strategy and the priorities, which is how layered
configuration (defaults, then system, then user) is expressed.

## Command line

A small helper that converts between the formats and extracts single values.

```console
$ go install github.com/go-freebsd/ucl/cmd/ucl@latest
```

Pull one value out of a file. A single value is printed plainly, so a shell can
use it as it stands:

```console
$ ucl -f /usr/local/etc/pkg.conf FreeBSD.url
pkg+https://pkg.FreeBSD.org/FreeBSD:14:amd64/quarterly
$ ucl -f /usr/local/etc/pkg.conf REPOS_DIR.1
/usr/local/etc/pkg/repos
```

Hand the document to something that only speaks JSON:

```console
$ ucl -o json-compact -f /usr/local/etc/pkg.conf | jq '.FreeBSD.enabled'
```

Or see what the parser actually makes of a layered configuration, with the
includes resolved, the variables expanded and the priorities applied:

```console
$ ucl -D ABI=FreeBSD:14:amd64 -I /usr/local/etc/myapp -f myapp.conf
```

It reads; it never writes a document back, so it is a convenience rather than a
drop in replacement for FreeBSD's `uclcmd(1)`. Run `ucl -h` for the full set of
flags.

## Compatibility with libucl

The lexer is written against libucl's own character tables, so the details
that bite in practice behave the same way:

* an unquoted value may contain spaces and runs to the end of the line, to a
  comment, or to an unbalanced `,` `;` `]` `}`, with trailing blanks removed;
* `1.2.3`, `127.0.0.1` and `10 20` stay strings, while `10s` is a time and
  `1kb` is 1024;
* a number is only attempted when the token starts with a digit or `-`, so
  `+3` and `.5` are strings;
* `1.5kb` truncates before applying the binary multiplier and yields 1024;
* heredoc terminators must be upper case and start a line;
* in single quoted strings only `\'` and a trailing backslash line
  continuation have a meaning;
* an unknown escape such as `\q` loses its backslash.

Known differences:

* **JSON Schema validation** (`ucl_object_validate`, draft 4) is not
  implemented. Validation here is driven by Go struct tags instead, which
  covers the same ground for the usual case of a document that is about to
  become a Go value, without a second schema document to parse. Schema
  features with no struct equivalent, such as `anyOf`, `dependencies` and
  `$ref`, have no counterpart.
* **Signed includes** (`.includes`) are rejected instead of silently accepted,
  and the `url` and `sign` parameters of `.include` are not supported, so no
  configuration is ever fetched over the network.
* **`\uXXXX` surrogate pairs** are decoded into one rune; libucl encodes each
  half separately, which produces invalid UTF-8.
* **YAML output** is block style, which is valid YAML but not identical to
  libucl's output byte for byte.
* **Comments are not preserved** across a parse and emit cycle.
* **Keys** are accepted a little more liberally than libucl's `KEY_START`
  class allows.
* **msgpack** extension types are not supported.

## Development

The library is in `pkg/ucl`, the command in `cmd/ucl`, and the fixtures the
tests read stay at the repository root in `testdata`, which is why the tests
name them as `../../testdata/…`.

The grammar lives in `pkg/ucl/ucl.y` and is processed with
[goyacc](https://pkg.go.dev/golang.org/x/tools/cmd/goyacc); the generated
`ucl.y.go` is checked in, so building the package never needs any tool beyond
the Go compiler. The context sensitive part, which is most of the work, is the
hand written scanner in `pkg/ucl/scanner.go`.

Documentation is kept honest by the test suite. The `example_*_test.go` files
hold 49 runnable examples that `go test` executes and compares against their
documented output, and `readme_test.go` checks that every README block marked
with `<!-- example: … -->` is byte for byte the body of the example it names.
Unmarked blocks, such as the ones below that read `/usr/local/etc/pkg.conf`,
are illustrations that cannot be run.

```console
$ make test       # go test ./..., examples included
$ make examples   # just the examples, showing what each one prints
$ make lint       # go vet and gofmt
$ make cover      # coverage, with and without the generated parser
$ make cover-html # the same profile as a browsable report
$ make fuzz       # 60 seconds of fuzzing by default
$ make generate   # regenerate ucl.y.go from pkg/ucl/ucl.y
```

`make cover` prints two figures. `ucl.y.go` is generated by goyacc and carries a
`//line` directive, which both makes `go tool cover` fail outright and puts
statements no test can reach into the total, so the profile is filtered and the
hand written figure reported alongside the raw one. CI prints the same summary
on every run.

## License

BSD 2-Clause, the same license as libucl. See [LICENSE](LICENSE).
