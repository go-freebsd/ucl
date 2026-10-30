package ucl

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// rules holds the validation constraints declared in a struct tag. Bounds are
// kept as written and only interpreted once the field type is known, so that
// a duration can be bounded with "30s" and a number with "30".
type rules struct {
	required   bool
	unique     bool
	min        string
	max        string
	length     string
	pattern    *regexp.Regexp
	enum       []string
	multipleOf string
	// defValue is the parsed default= option, or nil when none was given.
	defValue *Object
	// err reports a malformed tag, which is surfaced during validation.
	err string
}

// empty reports whether no constraint was declared at all.
func (r rules) empty() bool {
	return !r.required && !r.unique && r.min == "" && r.max == "" &&
		r.length == "" && r.pattern == nil && r.enum == nil &&
		r.multipleOf == "" && r.err == ""
}

// fieldInfo describes one struct field that takes part in encoding, decoding
// and validation.
type fieldInfo struct {
	name      string
	index     []int
	omitEmpty bool
	inline    bool
	rules     rules
}

var fieldCache sync.Map // reflect.Type -> []fieldInfo

// structFields returns the encodable fields of a struct type, honouring the
// "ucl" struct tag and falling back to "json". Embedded structs without a tag
// are flattened into the enclosing object, like encoding/json does.
//
// The tag vocabulary this parses is specified in the package documentation
// under Struct tags. Keep the two in step.
func structFields(t reflect.Type) []fieldInfo {
	if v, ok := fieldCache.Load(t); ok {
		return v.([]fieldInfo)
	}
	fields := collectFields(t, nil, make(map[reflect.Type]bool))
	fieldCache.Store(t, fields)
	return fields
}

func collectFields(t reflect.Type, index []int, seen map[reflect.Type]bool) []fieldInfo {
	if seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var out []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag, hasTag, isUCL := lookupTag(sf)
		if tag == "-" {
			continue
		}
		parts := splitTag(tag)
		name := parts[0]
		idx := append(append([]int(nil), index...), i)

		if sf.Anonymous && !hasTag {
			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				out = append(out, collectFields(ft, idx, seen)...)
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		f := fieldInfo{name: name, index: idx}
		f.parseOptions(parts[1:], isUCL)
		f.rules.validate()
		out = append(out, f)
	}
	return out
}

// lookupTag returns the struct tag that names the field and reports whether it
// came from the "ucl" tag, which is the only one that may carry constraints.
func lookupTag(sf reflect.StructField) (tag string, found, isUCL bool) {
	if tag, ok := sf.Tag.Lookup("ucl"); ok {
		return tag, true, true
	}
	if tag, ok := sf.Tag.Lookup("json"); ok {
		return tag, true, false
	}
	return "", false, false
}

// splitTag splits a struct tag on commas. A comma nested in brackets or braces
// does not split, so that a pattern such as [a-z]{2,4} survives intact.
func splitTag(tag string) []string {
	var (
		parts []string
		depth int
		start int
	)
	for i := 0; i < len(tag); i++ {
		switch tag[i] {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, tag[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, tag[start:])
}

// parseOptions decodes the options that follow the field name in a struct tag.
// Unknown options are an error in a "ucl" tag and are ignored in a "json" tag,
// which may legitimately carry options this package knows nothing about.
func (f *fieldInfo) parseOptions(opts []string, isUCL bool) {
	for _, opt := range opts {
		if opt == "" {
			continue
		}
		key, val, hasVal := strings.Cut(opt, "=")
		key = strings.ToLower(strings.TrimSpace(key))

		switch key {
		case "omitempty", "inline", "required", "unique":
			if hasVal {
				f.rules.fail("option %q takes no value", key)
				continue
			}
		case "min", "max", "len", "pattern", "enum", "multipleof", "default":
			if !hasVal || val == "" {
				f.rules.fail("option %q needs a value", key)
				continue
			}
		}

		switch key {
		case "omitempty":
			f.omitEmpty = true
		case "inline":
			f.inline = true
		case "required":
			f.rules.required = true
		case "unique":
			f.rules.unique = true
		case "min":
			f.rules.min = val
		case "max":
			f.rules.max = val
		case "len":
			f.rules.length = val
		case "multipleof":
			f.rules.multipleOf = val
		case "enum":
			f.rules.enum = strings.Split(val, "|")
		case "pattern":
			re, err := regexp.Compile(val)
			if err != nil {
				f.rules.fail("invalid pattern %q: %v", val, err)
				continue
			}
			f.rules.pattern = re
		case "default":
			// The default is written in the value syntax of the language
			// itself, so "4", "30s", "yes" and "[a, b]" all work.
			doc, err := ParseString("value = "+val, WithoutMacros())
			if err != nil {
				f.rules.fail("invalid default %q: %v", val, err)
				continue
			}
			d := doc.Get("value")
			if d == nil {
				f.rules.fail("invalid default %q", val)
				continue
			}
			f.rules.defValue = d
		default:
			if isUCL {
				f.rules.fail("unknown option %q in the ucl struct tag", key)
			}
		}
	}
}

// validate reports a combination of options that cannot be satisfied.
func (r *rules) validate() {
	if r.required && r.defValue != nil {
		r.fail("required and default cannot both be given")
	}
}

func (r *rules) fail(format string, args ...any) {
	if r.err == "" {
		r.err = fmt.Sprintf(format, args...)
	}
}

// fieldByIndex walks an index path, optionally allocating nil embedded
// pointers on the way. It returns an invalid Value when a nil pointer is met
// and alloc is false.
func fieldByIndex(v reflect.Value, index []int, alloc bool) reflect.Value {
	for i, x := range index {
		if i > 0 {
			for v.Kind() == reflect.Pointer {
				if v.IsNil() {
					if !alloc || !v.CanSet() {
						return reflect.Value{}
					}
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v
}

// fieldTypeByIndex is fieldByIndex on types.
func fieldTypeByIndex(t reflect.Type, index []int) reflect.Type {
	for i, x := range index {
		if i > 0 {
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
		}
		t = t.Field(x).Type
	}
	return t
}

var flatCache sync.Map // reflect.Type -> []fieldInfo

// flatFields returns the fields of a struct type with every field tagged
// inline replaced by the fields of the struct it holds, so that those are
// matched against the enclosing object rather than against one named after
// the field. The index paths it returns are relative to t, so
// [fieldByIndex] reaches the nested field directly.
//
// Decoding and validation both work from this view, which is what keeps them
// agreeing about where an inline field's contents live.
func flatFields(t reflect.Type) []fieldInfo {
	if v, ok := flatCache.Load(t); ok {
		return v.([]fieldInfo)
	}
	fields := expandInline(t, nil, make(map[reflect.Type]bool))
	flatCache.Store(t, fields)
	return fields
}

func expandInline(t reflect.Type, prefix []int, seen map[reflect.Type]bool) []fieldInfo {
	if seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var out []fieldInfo
	for _, f := range structFields(t) {
		idx := f.index
		if len(prefix) > 0 {
			idx = append(append([]int(nil), prefix...), f.index...)
		}
		if f.inline {
			if ft := fieldTypeByIndex(t, f.index); canInline(ft) {
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				out = append(out, expandInline(ft, idx, seen)...)
				continue
			}
		}
		f.index = idx
		out = append(out, f)
	}
	return out
}

// canInline reports whether the fields of a type can be folded into the
// enclosing object. Types that are decoded as a whole, such as *Object and
// anything that unmarshals from text, are left alone even when tagged inline.
func canInline(t reflect.Type) bool {
	if t == objectPointerTyp {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
		if t == objectPointerTyp {
			return false
		}
	}
	if t.Kind() != reflect.Struct || t == durationType {
		return false
	}
	return !isTextUnmarshaler(t)
}
