package ucl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:generate goyacc -o ucl.y.go -p yy ucl.y

// DefaultMaxIncludeDepth bounds how deeply .include macros may nest.
const DefaultMaxIncludeDepth = 32

// member is one entry of a container as produced by the grammar. It is either
// an ordinary key/value pair or a macro invocation.
type member struct {
	key    string
	val    *Object
	macro  string
	params *Object
}

// Parser turns UCL documents into objects. A parser may consume several
// documents, which are merged into a single result.
type Parser struct {
	vars       map[string]string
	varHandler VariableHandler
	macros     map[string]MacroHandler

	strategy Duplicate
	priority uint8
	paths    []string

	lowercaseKeys bool
	noImplicit    bool
	noTime        bool
	noMacros      bool
	noFileVars    bool
	maxDepth      int

	root  *Object
	depth int
	// active guards against include cycles.
	active map[string]bool
	// curFile and curDir drive the FILENAME and CURDIR variables.
	curFile string
	curDir  string
}

// Option configures a Parser.
type Option func(*Parser)

// WithVariable registers a variable available as $name or ${name}.
func WithVariable(name, value string) Option {
	return func(p *Parser) { p.vars[name] = value }
}

// WithVariables registers several variables at once.
func WithVariables(vars map[string]string) Option {
	return func(p *Parser) {
		for k, v := range vars {
			p.vars[k] = v
		}
	}
}

// WithVariableHandler installs a callback for variables that are not
// registered. Returning false leaves the reference untouched.
func WithVariableHandler(h VariableHandler) Option {
	return func(p *Parser) { p.varHandler = h }
}

// WithDuplicateStrategy selects how repeated keys are handled. The default is
// DuplicateAppend, which builds implicit arrays.
func WithDuplicateStrategy(d Duplicate) Option {
	return func(p *Parser) { p.strategy = d }
}

// WithPriority sets the priority assigned to the objects of the following
// documents. Values of a higher priority replace values of a lower one, and
// values of equal priority form an implicit array. Priorities above
// [MaxPriority] are clamped.
func WithPriority(prio uint8) Option {
	return func(p *Parser) {
		if prio > MaxPriority {
			prio = MaxPriority
		}
		p.priority = prio
	}
}

// WithIncludePath adds directories that .include searches for relative file
// names, in addition to the directory of the including file.
func WithIncludePath(dirs ...string) Option {
	return func(p *Parser) { p.paths = append(p.paths, dirs...) }
}

// WithLowercaseKeys converts every key to lower case, like the libucl flag
// UCL_PARSER_KEY_LOWERCASE.
func WithLowercaseKeys() Option {
	return func(p *Parser) { p.lowercaseKeys = true }
}

// WithoutImplicitArrays makes a repeated key replace the previous value
// instead of building an implicit array, like UCL_PARSER_NO_IMPLICIT_ARRAYS.
func WithoutImplicitArrays() Option {
	return func(p *Parser) { p.noImplicit = true }
}

// WithoutTime disables the time suffixes, so that values such as "10s" stay
// strings. It mirrors UCL_PARSER_NO_TIME.
func WithoutTime() Option {
	return func(p *Parser) { p.noTime = true }
}

// WithoutMacros disables macro processing, so that a leading dot is an
// ordinary key character. It mirrors UCL_PARSER_DISABLE_MACRO.
func WithoutMacros() Option {
	return func(p *Parser) { p.noMacros = true }
}

// WithoutFileVariables suppresses the automatic CURDIR and FILENAME
// variables, like UCL_PARSER_NO_FILEVARS.
func WithoutFileVariables() Option {
	return func(p *Parser) { p.noFileVars = true }
}

// WithMaxIncludeDepth limits how deeply .include macros may nest. The default
// is [DefaultMaxIncludeDepth]; a document that nests deeper fails to parse.
func WithMaxIncludeDepth(n int) Option {
	return func(p *Parser) { p.maxDepth = n }
}

// WithMacro registers a user defined macro. The name is given without the
// leading dot.
func WithMacro(name string, h MacroHandler) Option {
	return func(p *Parser) { p.macros[name] = h }
}

// NewParser returns a parser configured with the given options.
func NewParser(opts ...Option) *Parser {
	p := &Parser{
		vars:     make(map[string]string),
		macros:   make(map[string]MacroHandler),
		active:   make(map[string]bool),
		maxDepth: DefaultMaxIncludeDepth,
		curDir:   ".",
	}
	registerBuiltinMacros(p)
	for _, o := range opts {
		o(p)
	}
	return p
}

// SetVariable registers a variable after the parser was created.
func (p *Parser) SetVariable(name, value string) { p.vars[name] = value }

// RegisterMacro installs a macro handler after the parser was created.
func (p *Parser) RegisterMacro(name string, h MacroHandler) { p.macros[name] = h }

// Result returns the document parsed so far, or nil if nothing was parsed. The
// object is the parser's own, not a copy, so a later Add call may change it.
func (p *Parser) Result() *Object { return p.root }

// AddChunk parses an in memory document and merges it into the result.
func (p *Parser) AddChunk(data []byte) error {
	return p.addChunk(data, "")
}

// AddString parses an in memory document given as a string and merges it into
// the result.
func (p *Parser) AddString(s string) error { return p.addChunk([]byte(s), "") }

// AddFile reads and parses a file and merges it into the result.
func (p *Parser) AddFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return p.addChunk(data, path)
}

// AddReader reads a whole document from r, parses it and merges it into the
// result. The name appears in error messages and sets the FILENAME and CURDIR
// variables.
func (p *Parser) AddReader(r io.Reader, name string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return p.addChunk(data, name)
}

func (p *Parser) addChunk(data []byte, name string) error {
	obj, err := p.parseChunk(data, name, p.priority)
	if err != nil {
		return err
	}
	if p.root == nil {
		p.root = obj
		return nil
	}
	if p.root.Type() != TypeObject || obj.Type() != TypeObject {
		return fmt.Errorf("ucl: cannot merge a %s document into a %s document",
			obj.Type(), p.root.Type())
	}
	return p.merge(p.root, obj, p.strategy)
}

// parseChunk runs the generated parser over one document.
func (p *Parser) parseChunk(data []byte, name string, priority uint8) (*Object, error) {
	savedFile, savedDir := p.curFile, p.curDir
	if name != "" {
		abs, err := filepath.Abs(name)
		if err != nil {
			abs = name
		}
		p.curFile, p.curDir = abs, filepath.Dir(abs)
	}
	savedPrio := p.priority
	p.priority = priority
	p.setFileVars()
	defer func() {
		p.curFile, p.curDir = savedFile, savedDir
		p.priority = savedPrio
		p.setFileVars()
	}()

	l := newLexer(p, newChunk(data, name))
	if yyParse(l) != 0 && l.err == nil {
		l.err = l.errorf("syntax error")
	}
	if l.err != nil {
		return nil, l.err
	}
	if l.result == nil {
		return NewObject(), nil
	}
	return l.result, nil
}

func (p *Parser) setFileVars() {
	if p.noFileVars {
		return
	}
	if p.curFile != "" {
		p.vars["FILENAME"] = p.curFile
	}
	p.vars["CURDIR"] = p.curDir
}

// apply inserts one parsed member into its container. Errors are reported
// through the lexer because the generated parser has no way to return them.
func (l *lexer) apply(container *Object, m *member) {
	if m == nil || l.err != nil {
		return
	}
	if m.macro != "" {
		if err := l.p.runMacro(container, m); err != nil {
			l.err = err
		}
		return
	}
	key := m.key
	if l.p.lowercaseKeys {
		key = strings.ToLower(key)
	}
	m.val.priority = l.p.priority
	if err := l.p.insert(container, key, m.val, l.p.strategy); err != nil {
		l.err = l.errorf("%s", err)
	}
}

// insert honours the no-implicit-arrays setting on top of Object.Insert.
func (p *Parser) insert(container *Object, key string, val *Object, s Duplicate) error {
	if p.noImplicit && s == DuplicateAppend {
		s = DuplicateRewrite
	}
	return container.Insert(key, val, s)
}

// merge combines two objects honouring the parser settings.
func (p *Parser) merge(dst, src *Object, s Duplicate) error {
	for _, e := range src.Elems() {
		if err := p.insert(dst, e.key, e, s); err != nil {
			return err
		}
	}
	return nil
}

// Parse parses a UCL document held in memory.
func Parse(data []byte, opts ...Option) (*Object, error) {
	p := NewParser(opts...)
	if err := p.AddChunk(data); err != nil {
		return nil, err
	}
	return p.Result(), nil
}

// ParseString parses a UCL document given as a string.
func ParseString(s string, opts ...Option) (*Object, error) {
	return Parse([]byte(s), opts...)
}

// ParseFile reads and parses a UCL document from disk.
func ParseFile(path string, opts ...Option) (*Object, error) {
	p := NewParser(opts...)
	if err := p.AddFile(path); err != nil {
		return nil, err
	}
	return p.Result(), nil
}

// ErrNotFound is returned by includes when a file is missing and the macro
// was not marked as "try".
var ErrNotFound = errors.New("ucl: included file not found")
