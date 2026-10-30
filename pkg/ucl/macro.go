package ucl

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MacroContext carries everything a macro handler needs: the argument that
// follows the macro name, the parameters given in parentheses and the
// container the macro appeared in.
type MacroContext struct {
	// Parser is the parser that is processing the document.
	Parser *Parser
	// Name is the macro name without the leading dot.
	Name string
	// Value is the argument of the macro, usually a string.
	Value *Object
	// Params holds the parameters from the optional (key=value) list and may
	// be nil.
	Params *Object
	// Target is the object the macro was invoked in. A handler inserts its
	// results here.
	Target *Object
}

// MacroHandler implements a macro such as .include.
type MacroHandler func(ctx *MacroContext) error

func registerBuiltinMacros(p *Parser) {
	p.macros["include"] = includeMacro
	p.macros["try_include"] = tryIncludeMacro
	p.macros["includes"] = signedIncludeMacro
	p.macros["priority"] = priorityMacro
	p.macros["load"] = loadMacro
}

func (p *Parser) runMacro(container *Object, m *member) error {
	h, ok := p.macros[m.macro]
	if !ok {
		return fmt.Errorf("ucl: unknown macro .%s", m.macro)
	}
	return h(&MacroContext{
		Parser: p,
		Name:   m.macro,
		Value:  m.val,
		Params: m.params,
		Target: container,
	})
}

// includeParams holds the decoded parameter list of an include.
type includeParams struct {
	try       bool
	glob      bool
	prefix    bool
	key       string
	strategy  Duplicate
	priority  uint8
	paths     []string
	hasPrio   bool
	hasStrat  bool
	rawTarget string
}

func decodeIncludeParams(p *Parser, params *Object) (includeParams, error) {
	ip := includeParams{strategy: p.strategy, priority: p.priority}
	if params == nil {
		return ip, nil
	}
	for _, e := range params.Elems() {
		switch strings.ToLower(e.Key()) {
		case "try":
			ip.try = e.BoolOr(false)
		case "glob":
			ip.glob = e.BoolOr(false)
		case "prefix":
			ip.prefix = e.BoolOr(false)
		case "key":
			ip.key = e.ToString()
		case "target":
			ip.rawTarget = e.ToString()
		case "duplicate":
			s, err := parseDuplicate(e.ToString())
			if err != nil {
				return ip, err
			}
			ip.strategy, ip.hasStrat = s, true
		case "priority":
			v, ok := e.IntOK()
			if !ok || v < 0 || v > MaxPriority {
				return ip, fmt.Errorf("ucl: invalid include priority %q", e.ToString())
			}
			ip.priority, ip.hasPrio = uint8(v), true
		case "path":
			ip.paths = append(ip.paths, stringList(e)...)
		case "url", "sign":
			return ip, fmt.Errorf("ucl: include parameter %q is not supported", e.Key())
		}
	}
	return ip, nil
}

func parseDuplicate(s string) (Duplicate, error) {
	switch strings.ToLower(s) {
	case "append", "":
		return DuplicateAppend, nil
	case "merge":
		return DuplicateMerge, nil
	case "rewrite":
		return DuplicateRewrite, nil
	case "error":
		return DuplicateError, nil
	}
	return DuplicateAppend, fmt.Errorf("ucl: unknown duplicate strategy %q", s)
}

// stringList returns the string values of an object, flattening arrays.
func stringList(o *Object) []string {
	if o == nil {
		return nil
	}
	if o.Type() == TypeArray {
		var out []string
		for _, e := range o.Elems() {
			out = append(out, e.ToString())
		}
		return out
	}
	return []string{o.ToString()}
}

func includeMacro(ctx *MacroContext) error { return doInclude(ctx, false) }

func tryIncludeMacro(ctx *MacroContext) error { return doInclude(ctx, true) }

func signedIncludeMacro(ctx *MacroContext) error {
	return errors.New("ucl: .includes requires signature verification, which is not supported")
}

func doInclude(ctx *MacroContext, forceTry bool) error {
	p := ctx.Parser
	ip, err := decodeIncludeParams(p, ctx.Params)
	if err != nil {
		return err
	}
	if forceTry {
		ip.try = true
	}
	if p.depth >= p.maxDepth {
		return fmt.Errorf("ucl: include nested deeper than %d levels", p.maxDepth)
	}
	for _, pattern := range stringList(ctx.Value) {
		if pattern == "" {
			continue
		}
		files, err := p.resolveInclude(pattern, ip)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			if ip.try {
				continue
			}
			return fmt.Errorf("%w: %s", ErrNotFound, pattern)
		}
		for _, f := range files {
			if err := p.includeFile(f, ip, ctx.Target); err != nil {
				if ip.try && errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
		}
	}
	return nil
}

// resolveInclude turns the argument of an include into a list of files.
func (p *Parser) resolveInclude(pattern string, ip includeParams) ([]string, error) {
	var candidates []string
	if filepath.IsAbs(pattern) {
		candidates = []string{pattern}
	} else {
		dirs := append([]string{p.curDir}, ip.paths...)
		dirs = append(dirs, p.paths...)
		for _, d := range dirs {
			candidates = append(candidates, filepath.Join(d, pattern))
		}
	}
	var out []string
	for _, c := range candidates {
		if ip.glob {
			m, err := filepath.Glob(c)
			if err != nil {
				return nil, fmt.Errorf("ucl: bad include pattern %q: %w", pattern, err)
			}
			sort.Strings(m)
			out = append(out, m...)
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return []string{c}, nil
		}
	}
	return out, nil
}

// includeFile parses one included file into the target container.
func (p *Parser) includeFile(path string, ip includeParams, target *Object) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if p.active[abs] {
		return fmt.Errorf("ucl: include loop detected at %s", abs)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if ip.try && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	p.active[abs] = true
	p.depth++
	obj, err := p.parseChunk(data, path, ip.priority)
	p.depth--
	delete(p.active, abs)
	if err != nil {
		return err
	}
	if ip.prefix {
		key := ip.key
		if key == "" {
			key = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		obj.priority = ip.priority
		return p.insert(target, key, obj, ip.strategy)
	}
	if obj.Type() != TypeObject {
		return fmt.Errorf("ucl: included file %s is not an object", path)
	}
	return p.merge(target, obj, ip.strategy)
}

// priorityMacro implements ".priority N", which changes the priority used for
// the remainder of the current document.
func priorityMacro(ctx *MacroContext) error {
	v, ok := ctx.Value.IntOK()
	if !ok || v < 0 || v > MaxPriority {
		return fmt.Errorf("ucl: .priority needs a value between 0 and %d", MaxPriority)
	}
	ctx.Parser.priority = uint8(v)
	return nil
}

// loadMacro implements ".load", which reads a file and stores its contents as
// a value. The key parameter names the resulting field and defaults to the
// base name of the file.
func loadMacro(ctx *MacroContext) error {
	p := ctx.Parser
	ip, err := decodeIncludeParams(p, ctx.Params)
	if err != nil {
		return err
	}
	var encode, valType string
	ctx.Params.Each(func(k string, v *Object) bool {
		switch strings.ToLower(k) {
		case "encode":
			encode = strings.ToLower(v.ToString())
		case "type":
			valType = strings.ToLower(v.ToString())
		}
		return true
	})

	name := ctx.Value.ToString()
	files, err := p.resolveInclude(name, ip)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		if ip.try {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		if ip.try && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	key := ip.key
	if key == "" {
		key = strings.TrimSuffix(filepath.Base(files[0]), filepath.Ext(files[0]))
	}

	var val *Object
	switch encode {
	case "base64":
		val = NewString(base64.StdEncoding.EncodeToString(data))
	case "hex":
		val = NewString(hex.EncodeToString(data))
	case "", "none":
		s := strings.TrimRight(string(data), "\n")
		if valType == "int" {
			if n := parseNumber(strings.TrimSpace(s), false); n != nil {
				val = n
				break
			}
			return fmt.Errorf("ucl: .load cannot read %s as an integer", files[0])
		}
		val = NewString(s)
	default:
		return fmt.Errorf("ucl: unknown .load encoding %q", encode)
	}
	val.priority = ip.priority
	return p.insert(ctx.Target, key, val, ip.strategy)
}
