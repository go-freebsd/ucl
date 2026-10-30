// Package ucl implements the Universal Configuration Language as used by FreeBSD,
// in particular by pkg(8), uclcmd(1) and the configuration files below
// /usr/local/etc.
//
// The implementation follows libucl (https://github.com/vstakhov/libucl), the
// reference C implementation that ships in the FreeBSD base system: the same
// syntax, the same numeric and time suffixes, the same implicit arrays,
// priorities and duplicate handling, the same .include macros and the same output
// formats. It needs nothing outside the standard library.
//
// Most programs need two calls: [ParseFile] to read a document and
// [Object.Decode] to turn it into a Go value, or [UnmarshalFile] to do both at
// once.
//
// # The language
//
// UCL is a superset of JSON that reads like an nginx configuration file. A key is
// separated from its value by "=", by ":" or by nothing at all, and entries are
// separated by a comma, a semicolon or a newline:
//
//	# a comment, /* block comments nest */
//	name = "web frontend"
//	workers 4
//	timeout: 30s
//	section {
//		memory = 512mb       # 536870912, mb is binary and m is decimal
//		enabled = yes        # true/false, yes/no and on/off are booleans
//		hosts = [a, b, c]
//		raw = 'no $expansion here'
//		banner = <<EOD
//	a multiline string
//	EOD
//	}
//
// Repeating a key does not overwrite it, it builds an implicit array, which is
// how UCL expresses a multi valued option. See [Object.Values].
//
// # Parsing
//
// [Parse], [ParseString] and [ParseFile] read a single document. [NewParser]
// returns a [Parser] that accepts several documents through [Parser.AddFile] and
// its siblings and merges them into one result, which is how layered
// configuration is expressed: defaults first, then the system file, then the
// user's. Use [WithPriority] to decide which layer wins and
// [WithDuplicateStrategy] to decide how repeated keys combine.
//
// [Option] values configure a parser: variables for $VAR expansion, the include
// search path, lower casing of keys, and switches for the parts of the syntax a
// program would rather not accept. A parse failure is a [SyntaxError], which
// names the file, line and column.
//
// # The object model
//
// A document is a tree of [Object] values, each carrying one of the types in the
// [Type] enumeration. Containers keep their fields in document order.
//
// Read accessors tolerate a nil receiver, so a missing key is not a special case:
//
//	port := cfg.Get("server").Get("port").IntOr(8080)
//
// [Object.Get] returns a field, [Object.Lookup] follows a dotted path, and the
// Str, Int, Float, Bool and Duration accessors convert a scalar. Each has an OK
// variant that reports whether the conversion was possible and an Or variant that
// substitutes a default. Objects can also be built and modified in memory with
// [NewObject] and the other constructors.
//
// # Decoding into Go values
//
// [Object.Decode], [Unmarshal] and [UnmarshalFile] fill a Go value the way
// encoding/json does, and [Marshal] and [FromInterface] go the other way. Fields
// absent from the document are left alone, so a struct can be initialised with
// defaults before decoding. A scalar is accepted where a slice is expected, which
// makes implicit arrays convenient to consume.
//
// # Struct tags
//
// One tag describes a field for all three of decoding, encoding and validation.
// The field name comes first, then options separated by commas:
//
//	Workers int `ucl:"workers,required,min=1,max=64"`
//
// A "ucl" tag is used if present, otherwise a "json" tag, otherwise the field
// name matched case insensitively. A name of "-" skips the field.
//
// Options that control how a field is written and read:
//
//   - omitempty omits a field holding the zero value of its type when
//     encoding. It has no meaning when decoding.
//   - inline folds the fields of a nested struct into the enclosing object.
//     Encoding, decoding and validation all look for them there. A type that
//     is read as a whole, such as *[Object] or anything that unmarshals from
//     text, keeps its own key even when it carries this option.
//   - default=v gives [Object.Decode] a value for a key the document does not
//     mention. It is written in the value syntax of the language, so
//     default=30s, default=8mb, default=yes and default=[a, b] all work.
//     A field the caller filled in beforehand keeps that value, so the two
//     ways of expressing a default compose. It cannot be combined with
//     required, which says the opposite, and [Validate] reports a default
//     that the constraints on the same field reject.
//
// Options that declare constraints for [Validate]:
//
//   - required: the key must be present.
//   - min, max: inclusive bounds. On a number they bound the value, on a string
//     the length in runes, and on a slice, array or map the number of elements.
//   - len: an exact value, length or count.
//   - unique: a slice or array must not repeat an element.
//   - pattern: a regular expression the string must match, in Go syntax.
//   - enum: the accepted values, separated by a vertical bar.
//   - multipleof: the value must divide evenly.
//
// The last three describe a single value. On a slice, an array or a map they
// are applied to each element in turn, and a problem names the element it was
// found in, such as hosts[1] or levels.db. The others describe the field as a
// whole and keep counting the elements of a list.
//
// Bounds may be written with the suffixes a document uses, so max=64mb bounds a
// byte count and min=1s bounds a time.Duration field. A comma nested in brackets
// does not separate options, which keeps a pattern such as [a-z]{2,4} intact.
//
// An unrecognised option in a "ucl" tag is reported as an error. The same option
// in a "json" tag is ignored, so a tag written for encoding/json stays harmless.
//
// # Validation
//
// [Validate] checks a document against the constraints in the struct tags of a
// type, so there is no second schema document to write and keep in step with the
// Go type. It reports every problem it finds, not only the first, as
// [ValidationErrors], each naming the key it came from. [RejectUnknownKeys] adds
// the equivalent of "additionalProperties": false, which catches misspelled
// options.
//
// libucl validates with a JSON Schema draft 4 document instead. That is not
// implemented here.
//
// # Emitting
//
// [Object.Emit] and [Object.EmitTo] render a document in any of the formats in
// the [Format] enumeration: UCL configuration syntax, indented or compact JSON,
// block style YAML, or binary msgpack. [ParseMsgPack] reads msgpack back.
// [Object.MarshalJSON] lets an object be used directly with encoding/json.
//
// # Includes and macros
//
// A document can pull in others:
//
//	.include(try=true, glob=true) "conf.d/*.conf"
//
// Relative names are resolved against the directory of the including file and
// then against [WithIncludePath]. The parameters try, glob, prefix, key, path,
// duplicate and priority are supported, as are the .try_include, .priority and
// .load macros. The automatic variables CURDIR and FILENAME describe the file
// being parsed.
//
// [WithMacro] registers a macro of your own; its handler receives a
// [MacroContext] holding the argument, the parameters and the object the macro
// appeared in.
//
// Signed includes (.includes) are rejected rather than silently accepted, and
// there is no support for including over the network, so parsing a document never
// reaches out to anything but the local filesystem.
//
// # Compatibility with libucl
//
// The scanner is written against libucl's own character tables, so the details
// that matter in practice agree: an unquoted value may contain spaces and runs to
// the end of the line, to a comment, or to an unbalanced separator; 1.2.3 and
// 127.0.0.1 stay strings while 10s is a time and 1kb is 1024; a number is only
// attempted when a token starts with a digit or a minus sign. The README lists
// the remaining differences.
package ucl
