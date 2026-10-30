package ucl

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Format selects the output syntax of an emitter.
type Format uint8

const (
	// JSON is indented JSON, the equivalent of UCL_EMIT_JSON.
	JSON Format = iota
	// JSONCompact is JSON without any superfluous whitespace, the
	// equivalent of UCL_EMIT_JSON_COMPACT.
	JSONCompact
	// Config is UCL configuration syntax, the equivalent of
	// UCL_EMIT_CONFIG. It is the format uclcmd and pkg.conf use.
	Config
	// YAML is block style YAML, the equivalent of UCL_EMIT_YAML.
	YAML
	// MsgPack is the binary msgpack encoding, the equivalent of
	// UCL_EMIT_MSGPACK.
	MsgPack
)

// String implements fmt.Stringer.
func (f Format) String() string {
	switch f {
	case JSON:
		return "json"
	case JSONCompact:
		return "json-compact"
	case Config:
		return "config"
	case YAML:
		return "yaml"
	case MsgPack:
		return "msgpack"
	}
	return "unknown"
}

// Emit renders the object in the given format. A [TypeUserData] object is
// written as null. It returns an error for a format this package does not
// know.
func (o *Object) Emit(f Format) ([]byte, error) {
	var buf bytes.Buffer
	if err := o.EmitTo(&buf, f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EmitTo renders the object in the given format and writes it to w. The
// document is rendered in full before anything is written, so a failing writer
// never leaves a half emitted document behind.
func (o *Object) EmitTo(w io.Writer, f Format) error {
	var buf bytes.Buffer
	switch f {
	case JSON:
		emitJSON(&buf, o, 0, true)
		buf.WriteByte('\n')
	case JSONCompact:
		emitJSON(&buf, o, 0, false)
	case Config:
		emitConfig(&buf, o, 0, true)
	case YAML:
		emitYAML(&buf, o, 0, true)
	case MsgPack:
		if err := emitMsgPack(&buf, o); err != nil {
			return err
		}
	default:
		return fmt.Errorf("ucl: unknown emit format %d", f)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// MarshalJSON lets objects be used with the encoding/json package. It renders
// the object as [JSONCompact].
func (o *Object) MarshalJSON() ([]byte, error) { return o.Emit(JSONCompact) }

func writeIndent(b *bytes.Buffer, level, width int) {
	for i := 0; i < level*width; i++ {
		b.WriteByte(' ')
	}
}

// emitScalar writes the JSON representation of a scalar.
func emitScalar(b *bytes.Buffer, o *Object) {
	switch o.Type() {
	case TypeString:
		writeQuoted(b, o.sv)
	case TypeInt:
		b.WriteString(strconv.FormatInt(o.iv, 10))
	case TypeFloat, TypeTime:
		b.WriteString(formatFloat(o.dv))
	case TypeBoolean:
		b.WriteString(strconv.FormatBool(o.bv))
	default:
		b.WriteString("null")
	}
}

// writeQuoted writes a JSON string literal.
func writeQuoted(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			default:
				if c < 0x20 {
					fmt.Fprintf(b, `\u%04x`, c)
				} else {
					b.WriteByte(c)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(b, `\u%04x`, s[i])
			i++
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	b.WriteByte('"')
}

// emitJSON renders JSON, indented with four spaces when pretty is set.
func emitJSON(b *bytes.Buffer, o *Object, level int, pretty bool) {
	nl, sp := "", ""
	if pretty {
		nl, sp = "\n", " "
	}
	switch o.Type() {
	case TypeObject:
		if len(o.elems) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{" + nl)
		for i, e := range o.elems {
			if pretty {
				writeIndent(b, level+1, 4)
			}
			writeQuoted(b, e.key)
			b.WriteString(":" + sp)
			emitJSON(b, e, level+1, pretty)
			if i < len(o.elems)-1 {
				b.WriteByte(',')
			}
			b.WriteString(nl)
		}
		if pretty {
			writeIndent(b, level, 4)
		}
		b.WriteByte('}')
	case TypeArray:
		if len(o.elems) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[" + nl)
		for i, e := range o.elems {
			if pretty {
				writeIndent(b, level+1, 4)
			}
			emitJSON(b, e, level+1, pretty)
			if i < len(o.elems)-1 {
				b.WriteByte(',')
			}
			b.WriteString(nl)
		}
		if pretty {
			writeIndent(b, level, 4)
		}
		b.WriteByte(']')
	default:
		emitScalar(b, o)
	}
}

// emitConfig renders UCL configuration syntax. The top level object is
// written without the enclosing braces, the way libucl does it.
func emitConfig(b *bytes.Buffer, o *Object, level int, top bool) {
	switch o.Type() {
	case TypeObject:
		if !top {
			b.WriteString("{\n")
		}
		inner := level
		if !top {
			inner = level + 1
		}
		for _, e := range o.elems {
			writeIndent(b, inner, 4)
			writeQuoted(b, e.key)
			emitConfigValue(b, e, inner)
		}
		if !top {
			writeIndent(b, level, 4)
			b.WriteByte('}')
		}
	case TypeArray:
		emitConfigArray(b, o, level)
	default:
		emitScalar(b, o)
		b.WriteByte('\n')
	}
}

func emitConfigValue(b *bytes.Buffer, o *Object, level int) {
	switch o.Type() {
	case TypeObject:
		b.WriteByte(' ')
		emitConfig(b, o, level, false)
		b.WriteByte('\n')
	case TypeArray:
		b.WriteByte(' ')
		emitConfigArray(b, o, level)
		b.WriteByte('\n')
	default:
		b.WriteString(" = ")
		emitScalar(b, o)
		b.WriteString(";\n")
	}
}

func emitConfigArray(b *bytes.Buffer, o *Object, level int) {
	if len(o.elems) == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteString("[\n")
	for _, e := range o.elems {
		writeIndent(b, level+1, 4)
		switch e.Type() {
		case TypeObject:
			emitConfig(b, e, level+1, false)
		case TypeArray:
			emitConfigArray(b, e, level+1)
		default:
			emitScalar(b, e)
		}
		b.WriteString(",\n")
	}
	writeIndent(b, level, 4)
	b.WriteByte(']')
}

// emitYAML renders block style YAML.
func emitYAML(b *bytes.Buffer, o *Object, level int, top bool) {
	switch o.Type() {
	case TypeObject:
		if len(o.elems) == 0 {
			b.WriteString("{}\n")
			return
		}
		for _, e := range o.elems {
			writeIndent(b, level, 2)
			b.WriteString(yamlKey(e.key))
			b.WriteByte(':')
			emitYAMLValue(b, e, level)
		}
	case TypeArray:
		if len(o.elems) == 0 {
			b.WriteString("[]\n")
			return
		}
		for _, e := range o.elems {
			writeIndent(b, level, 2)
			switch e.Type() {
			case TypeObject, TypeArray:
				if e.Len() == 0 {
					b.WriteString("- ")
					emitYAML(b, e, 0, false)
					continue
				}
				// Start the nested block on the next line.
				b.WriteString("-\n")
				emitYAML(b, e, level+1, false)
			default:
				b.WriteString("- ")
				b.WriteString(yamlScalar(e))
				b.WriteByte('\n')
			}
		}
	default:
		b.WriteString(yamlScalar(o))
		b.WriteByte('\n')
	}
}

func emitYAMLValue(b *bytes.Buffer, o *Object, level int) {
	switch o.Type() {
	case TypeObject, TypeArray:
		if o.Len() == 0 {
			b.WriteByte(' ')
			emitYAML(b, o, 0, false)
			return
		}
		b.WriteByte('\n')
		emitYAML(b, o, level+1, false)
	default:
		b.WriteByte(' ')
		b.WriteString(yamlScalar(o))
		b.WriteByte('\n')
	}
}

func yamlKey(s string) string {
	if needsYAMLQuote(s) {
		var b bytes.Buffer
		writeQuoted(&b, s)
		return b.String()
	}
	return s
}

func yamlScalar(o *Object) string {
	switch o.Type() {
	case TypeString:
		if needsYAMLQuote(o.sv) {
			var b bytes.Buffer
			writeQuoted(&b, o.sv)
			return b.String()
		}
		return o.sv
	default:
		var b bytes.Buffer
		emitScalar(&b, o)
		return b.String()
	}
}

// needsYAMLQuote reports whether a plain scalar would be ambiguous in YAML.
func needsYAMLQuote(s string) bool {
	if s == "" {
		return true
	}
	if s != strings.TrimSpace(s) {
		return true
	}
	switch s {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "True", "False", "Null", "NULL", "Yes", "No":
		return true
	}
	if parseNumber(s, true) != nil {
		return true
	}
	// Only a conservative set of leading characters is left unquoted, so that
	// the result is unambiguous both for a YAML reader and for this parser.
	if c := s[0]; !(c == '_' || c == '/' || c == '.' || isDigit(c) ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= utf8.RuneSelf) {
		return true
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c < 0x20:
			return true
		case c == ':' && (i+1 == len(s) || s[i+1] == ' '):
			return true
		case c == '#' && i > 0 && s[i-1] == ' ':
			return true
		}
	}
	return false
}
