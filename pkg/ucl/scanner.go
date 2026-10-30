package ucl

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The lexer needs to know whether a key or a value is expected, because UCL
// scans the two very differently: a key ends at the first separator while an
// unquoted value runs to the end of the line and may contain spaces.
const (
	expectKey = iota
	expectSep
	expectValue
	expectMacroParams
)

// chunk is one source document together with the position of the scanner.
type chunk struct {
	data      []byte
	pos       int
	line      int
	lineStart int
	name      string
}

func newChunk(data []byte, name string) *chunk {
	return &chunk{data: data, name: name, line: 1}
}

func (c *chunk) eof() bool { return c.pos >= len(c.data) }

func (c *chunk) at(off int) byte {
	if c.pos+off < len(c.data) {
		return c.data[c.pos+off]
	}
	return 0
}

func (c *chunk) advance(n int) {
	for i := 0; i < n && c.pos < len(c.data); i++ {
		if c.data[c.pos] == '\n' {
			c.line++
			c.lineStart = c.pos + 1
		}
		c.pos++
	}
}

func (c *chunk) column() int { return c.pos - c.lineStart + 1 }

// SyntaxError describes a problem found while parsing a document.
type SyntaxError struct {
	// File is the name the document was parsed under. It is empty for a
	// document that did not come from a file, and [SyntaxError.Error] then
	// writes "<input>" in its place.
	File string
	// Line is the 1 based line the problem was found on.
	Line int
	// Column is the 1 based column the problem was found at.
	Column int
	// Msg describes the problem.
	Msg string
}

// Error implements the error interface.
func (e *SyntaxError) Error() string {
	file := e.File
	if file == "" {
		file = "<input>"
	}
	return fmt.Sprintf("%s:%d:%d: %s", file, e.Line, e.Column, e.Msg)
}

// lexer implements the yyLexer interface expected by the generated parser.
type lexer struct {
	p      *Parser
	c      *chunk
	stack  []byte // container stack, 0 is the implicit top level object
	expect int
	result *Object
	err    error
}

func newLexer(p *Parser, c *chunk) *lexer {
	return &lexer{p: p, c: c, stack: []byte{0}, expect: expectKey}
}

func (l *lexer) errorf(format string, args ...any) error {
	return &SyntaxError{
		File:   l.c.name,
		Line:   l.c.line,
		Column: l.c.column(),
		Msg:    fmt.Sprintf(format, args...),
	}
}

// Error is called by the generated parser on a syntax error.
func (l *lexer) Error(s string) {
	if l.err == nil {
		l.err = l.errorf("%s", s)
	}
}

func (l *lexer) fail(err error) int {
	if l.err == nil {
		l.err = err
	}
	return tokError
}

func (l *lexer) push(c byte) { l.stack = append(l.stack, c) }

func (l *lexer) pop() {
	if len(l.stack) > 1 {
		l.stack = l.stack[:len(l.stack)-1]
	}
}

func (l *lexer) top() byte { return l.stack[len(l.stack)-1] }

// afterValue selects the mode that follows a completed value: inside an array
// another value may come, everywhere else a key is expected.
func (l *lexer) afterValue() {
	if l.top() == '[' {
		l.expect = expectValue
	} else {
		l.expect = expectKey
	}
}

// Lex returns the next token to the generated parser.
func (l *lexer) Lex(lval *yySymType) int {
	for {
		if err := l.skipSpace(); err != nil {
			return l.fail(err)
		}
		if l.c.eof() {
			return 0
		}
		ch := l.c.at(0)

		switch l.expect {
		case expectSep:
			if ch == '=' || ch == ':' {
				l.c.advance(1)
				l.expect = expectValue
				return tokEq
			}
			l.expect = expectValue
			continue
		case expectMacroParams:
			if ch == '(' {
				l.c.advance(1)
				l.push('(')
				l.expect = expectKey
				return tokOpenParen
			}
			l.expect = expectValue
			continue
		}

		switch ch {
		case '{':
			l.c.advance(1)
			l.push('{')
			l.expect = expectKey
			return tokOpenBrace
		case '[':
			l.c.advance(1)
			l.push('[')
			l.expect = expectValue
			return tokOpenBracket
		case '}':
			l.c.advance(1)
			l.pop()
			l.afterValue()
			return tokCloseBrace
		case ']':
			l.c.advance(1)
			l.pop()
			l.afterValue()
			return tokCloseBracket
		case ')':
			// Outside a macro parameter list a parenthesis is an ordinary
			// value character.
			if l.top() == '(' {
				l.c.advance(1)
				l.pop()
				l.expect = expectValue
				return tokCloseParen
			}
		case ',', ';':
			l.c.advance(1)
			l.afterValue()
			return tokSep
		}

		if l.expect == expectKey {
			if ch == '.' && isWordChar(l.c.at(1)) && !l.p.noMacros {
				l.c.advance(1)
				lval.str = l.scanWord()
				l.expect = expectMacroParams
				return tokMacro
			}
			key, err := l.scanKey()
			if err != nil {
				return l.fail(err)
			}
			lval.str = key
			l.expect = expectSep
			return tokKey
		}

		obj, err := l.scanValue()
		if err != nil {
			return l.fail(err)
		}
		lval.obj = obj
		l.afterValue()
		return tokScalar
	}
}

// skipSpace consumes whitespace, # comments and nested /* */ comments.
func (l *lexer) skipSpace() error {
	c := l.c
	for !c.eof() {
		switch ch := c.at(0); {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' || ch == '\v':
			c.advance(1)
		case ch == '#':
			for !c.eof() && c.at(0) != '\n' {
				c.advance(1)
			}
		case ch == '/' && c.at(1) == '*':
			if err := l.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// skipBlockComment consumes a /* */ comment. libucl allows them to nest.
func (l *lexer) skipBlockComment() error {
	c := l.c
	startLine := c.line
	depth := 0
	for !c.eof() {
		switch {
		case c.at(0) == '/' && c.at(1) == '*':
			depth++
			c.advance(2)
		case c.at(0) == '*' && c.at(1) == '/':
			depth--
			c.advance(2)
			if depth == 0 {
				return nil
			}
		default:
			c.advance(1)
		}
	}
	return &SyntaxError{
		File: c.name, Line: startLine, Column: 1,
		Msg: "unterminated comment",
	}
}

// isWordChar reports whether c may appear in a macro name.
func isWordChar(c byte) bool {
	return c == '_' || isDigit(c) ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// scanWord reads a plain identifier, used for macro names.
func (l *lexer) scanWord() string {
	start := l.c.pos
	for !l.c.eof() && isWordChar(l.c.at(0)) {
		l.c.advance(1)
	}
	return string(l.c.data[start:l.c.pos])
}

// isKeyTerminator reports whether c ends an unquoted key.
func isKeyTerminator(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '=', ':', ',', ';', '{', '}', '[', ']', '(', ')', '#':
		return true
	}
	return false
}

// scanKey reads a key, which may be quoted.
func (l *lexer) scanKey() (string, error) {
	switch l.c.at(0) {
	case '"':
		s, err := l.scanQuoted()
		return s, err
	case '\'':
		return l.scanSingleQuoted()
	}
	start := l.c.pos
	for !l.c.eof() && !isKeyTerminator(l.c.at(0)) {
		l.c.advance(1)
	}
	if l.c.pos == start {
		return "", l.errorf("unexpected character %q where a key was expected", string(l.c.at(0)))
	}
	return string(l.c.data[start:l.c.pos]), nil
}

// scanValue reads one scalar value.
func (l *lexer) scanValue() (*Object, error) {
	switch {
	case l.c.at(0) == '"':
		s, err := l.scanQuoted()
		if err != nil {
			return nil, err
		}
		return NewString(l.expand(s)), nil
	case l.c.at(0) == '\'':
		s, err := l.scanSingleQuoted()
		if err != nil {
			return nil, err
		}
		o := NewString(s)
		o.flags |= FlagSingleQuoted
		return o, nil
	case l.c.at(0) == '<' && l.c.at(1) == '<':
		if s, ok, err := l.scanHeredoc(); ok || err != nil {
			if err != nil {
				return nil, err
			}
			o := NewString(l.expand(s))
			o.flags |= FlagMultiline
			return o, nil
		}
	}
	return l.scanBareValue()
}

// scanBareValue reads an unquoted value. Such a value may contain spaces and
// extends to the end of the line, to a comment, or to one of the terminators
// , ; ] } that is not balanced by an opening bracket inside the value. This
// mirrors the UCL_CHARACTER_VALUE_END class of libucl. Trailing whitespace is
// removed and a backslash escapes the character that follows it.
func (l *lexer) scanBareValue() (*Object, error) {
	c := l.c
	start := c.pos
	escaped := false
	var square, figure int
loop:
	for !c.eof() {
		switch c.at(0) {
		case '\n', '\r', ',', ';', '#':
			break loop
		case '\\':
			escaped = true
			c.advance(1)
			if c.eof() {
				break loop
			}
		case '[':
			square++
		case ']':
			if square == 0 {
				break loop
			}
			square--
		case '{':
			figure++
		case '}':
			if figure == 0 {
				break loop
			}
			figure--
		case ')':
			// Only inside a macro parameter list does a closing
			// parenthesis end a value.
			if l.top() == '(' {
				break loop
			}
		case '/':
			if c.at(1) == '*' {
				break loop
			}
		}
		c.advance(1)
	}
	raw := strings.TrimRight(string(c.data[start:c.pos]), " \t\r\n\f\v")
	return l.classify(raw, escaped), nil
}

// classify turns a bare token into a typed object.
func (l *lexer) classify(s string, escaped bool) *Object {
	switch s {
	case "null":
		return NewNull()
	case "nan":
		return NewFloat(math.NaN())
	case "inf":
		return NewFloat(math.Inf(1))
	}
	if v, ok := parseBool(s); ok {
		return NewBool(v)
	}
	if o := parseNumber(s, !l.p.noTime); o != nil {
		return o
	}
	if escaped {
		s = unescapeString(s)
	}
	return NewString(l.expand(s))
}

// unescapeString decodes the backslash escapes of an unquoted value. It
// accepts the same sequences as a double quoted string.
func unescapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '"', '\\':
			b.WriteByte(c)
		default:
			// libucl drops the backslash of an unknown escape and
			// keeps the character that follows it.
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (l *lexer) expand(s string) string {
	return expandVars(s, l.p.vars, l.p.varHandler)
}

// scanQuoted reads a double quoted string and decodes its escapes.
func (l *lexer) scanQuoted() (string, error) {
	c := l.c
	startLine, startCol := c.line, c.column()
	c.advance(1) // opening quote
	var b strings.Builder
	for {
		if c.eof() {
			return "", &SyntaxError{File: c.name, Line: startLine, Column: startCol,
				Msg: "unterminated string"}
		}
		ch := c.at(0)
		switch ch {
		case '"':
			c.advance(1)
			return b.String(), nil
		case '\\':
			if err := l.unescape(&b); err != nil {
				return "", err
			}
		default:
			b.WriteByte(ch)
			c.advance(1)
		}
	}
}

// unescape decodes one backslash escape of a double quoted string.
func (l *lexer) unescape(b *strings.Builder) error {
	c := l.c
	c.advance(1) // backslash
	if c.eof() {
		return l.errorf("truncated escape sequence")
	}
	ch := c.at(0)
	c.advance(1)
	switch ch {
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case '"', '\\':
		b.WriteByte(ch)
	case 'u':
		r, err := l.scanUnicodeEscape()
		if err != nil {
			return err
		}
		b.WriteRune(r)
	default:
		// libucl drops the backslash of an unknown escape and keeps the
		// character that follows it.
		b.WriteByte(ch)
	}
	return nil
}

// scanUnicodeEscape decodes \uXXXX, combining a surrogate pair into the rune
// it stands for. A stray half becomes the replacement character and whatever
// follows it is left to be scanned normally.
func (l *lexer) scanUnicodeEscape() (rune, error) {
	c := l.c
	r, err := l.hex4()
	if err != nil {
		return 0, err
	}
	if !utf16.IsSurrogate(r) {
		return r, nil
	}
	// Only a high half can open a pair, and only when another \u escape
	// follows it. utf16.IsSurrogate is true for a low half as well, so
	// testing it alone would consume the next escape and lose it.
	if r <= 0xDBFF && c.at(0) == '\\' && c.at(1) == 'u' {
		saved := *c
		c.advance(2)
		r2, err := l.hex4()
		if err != nil {
			return 0, err
		}
		if dec := utf16.DecodeRune(r, r2); dec != utf8.RuneError {
			return dec, nil
		}
		// The second escape was not a low half after all, so put it back.
		*c = saved
	}
	return utf8.RuneError, nil
}

func (l *lexer) hex4() (rune, error) {
	c := l.c
	var r rune
	for i := 0; i < 4; i++ {
		if !isHexDigit(c.at(0)) {
			return 0, l.errorf("invalid unicode escape sequence")
		}
		r = r<<4 | rune(hexVal(c.at(0)))
		c.advance(1)
	}
	return r, nil
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// scanSingleQuoted reads a single quoted string. Only \' and \\ are decoded,
// every other backslash is literal and no variables are expanded.
func (l *lexer) scanSingleQuoted() (string, error) {
	c := l.c
	startLine, startCol := c.line, c.column()
	c.advance(1)
	var b strings.Builder
	for {
		if c.eof() {
			return "", &SyntaxError{File: c.name, Line: startLine, Column: startCol,
				Msg: "unterminated string"}
		}
		switch c.at(0) {
		case '\'':
			c.advance(1)
			return b.String(), nil
		case '\\':
			// libucl only gives a meaning to \' and to a backslash that
			// continues a line, every other backslash stays literal.
			switch c.at(1) {
			case '\'':
				b.WriteByte('\'')
				c.advance(2)
			case '\n':
				c.advance(2)
			case '\r':
				c.advance(2)
				if c.at(0) == '\n' {
					c.advance(1)
				}
			default:
				b.WriteByte('\\')
				c.advance(1)
			}
		default:
			b.WriteByte(c.at(0))
			c.advance(1)
		}
	}
}

// scanHeredoc reads a <<EOD ... EOD multiline string. Like libucl it only
// accepts upper case terminators that are followed directly by a newline, and
// the closing terminator has to start a line and be followed by a newline, a
// semicolon, a comma or the end of the document.
//
// It reports ok as false when what follows << is not a valid terminator, in
// which case nothing is consumed and the token is scanned as an ordinary
// value.
func (l *lexer) scanHeredoc() (string, bool, error) {
	c := l.c
	p := c.pos + 2
	start := p
	for p < len(c.data) && c.data[p] >= 'A' && c.data[p] <= 'Z' {
		p++
	}
	if p == start {
		return "", false, nil
	}
	term := c.data[start:p]
	if p < len(c.data) && c.data[p] == '\r' {
		p++
	}
	if p >= len(c.data) || c.data[p] != '\n' {
		return "", false, nil
	}
	startLine := c.line
	c.advance(p + 1 - c.pos)

	body := c.pos
	for pos := c.pos; pos <= len(c.data); pos++ {
		if pos > c.pos && c.data[pos-1] != '\n' {
			continue
		}
		if !bytes.HasPrefix(c.data[pos:], term) {
			continue
		}
		after := pos + len(term)
		if after < len(c.data) {
			switch c.data[after] {
			case '\n', '\r', ';', ',':
			default:
				continue
			}
		}
		end := pos
		if end > body {
			end-- // drop the newline that precedes the terminator
		}
		s := string(c.data[body:end])
		c.advance(after - c.pos)
		return s, true, nil
	}
	return "", true, &SyntaxError{File: c.name, Line: startLine, Column: 1,
		Msg: fmt.Sprintf("unterminated multiline string, %q not found", term)}
}
