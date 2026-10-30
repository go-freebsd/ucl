package ucl

import (
	"encoding"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidationError reports one constraint that a document does not satisfy.
type ValidationError struct {
	// Path names the offending value, for example "database.port".
	Path string
	// Msg describes what is wrong with it.
	Msg string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Path == "" {
		return "ucl: " + e.Msg
	}
	return "ucl: " + e.Path + ": " + e.Msg
}

// ValidationErrors is the list of every problem found in one document, in
// document order.
type ValidationErrors []*ValidationError

// Error implements the error interface. One problem is rendered on a single
// line, several are listed one per line, and an empty list reports that the
// document is valid.
func (e ValidationErrors) Error() string {
	switch len(e) {
	case 0:
		return "ucl: document is valid"
	case 1:
		return e[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ucl: %d validation errors:", len(e))
	for _, err := range e {
		b.WriteString("\n\t")
		if err.Path != "" {
			b.WriteString(err.Path + ": ")
		}
		b.WriteString(err.Msg)
	}
	return b.String()
}

// ValidateOption configures Validate.
type ValidateOption func(*validateConfig)

type validateConfig struct {
	rejectUnknown bool
}

// RejectUnknownKeys makes validation report keys that no struct field
// corresponds to. It is the equivalent of "additionalProperties": false and
// catches misspelled options in configuration files. A key counts as known
// when a field of the struct, or of a field tagged inline, carries that name.
func RejectUnknownKeys() ValidateOption {
	return func(c *validateConfig) { c.rejectUnknown = true }
}

var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// Validate checks a document against the constraints declared in the struct
// tags of v, which must be a struct or a pointer to one. The struct is only
// used for its type, so a zero value is enough:
//
//	type Config struct {
//		Workers int           `ucl:"workers,required,min=1,max=64"`
//		Mode    string        `ucl:"mode,enum=fast|safe"`
//		Timeout time.Duration `ucl:"timeout,min=1s,max=10min"`
//		Hosts   []string      `ucl:"hosts,min=1,unique"`
//	}
//
//	if err := ucl.Validate(obj, Config{}); err != nil {
//		return err
//	}
//
// The constraint options are required, min, max, len, unique, pattern, enum
// and multipleof. The package documentation describes them under Struct tags.
// On a slice, an array or a map, pattern, enum and multipleof apply to each
// element while the others describe the collection. Validate also reports a
// value that could not be decoded into the field at all, such as a string
// where a number belongs, a malformed struct tag, and a default= value its own
// field's constraints reject. Those three are properties of the struct rather
// than of the document, so they are reported whatever the document contains.
//
// Every problem is reported, not just the first. Validate returns nil when the
// document satisfies every constraint and otherwise a [ValidationErrors]
// holding one [ValidationError] per problem, in document order.
func Validate(o *Object, v any, opts ...ValidateOption) error {
	t := reflect.TypeOf(v)
	if t == nil {
		return fmt.Errorf("ucl: Validate needs a struct or a pointer to one, got nil")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("ucl: Validate needs a struct or a pointer to one, got %s", t)
	}
	vl := &validator{}
	for _, opt := range opts {
		opt(&vl.cfg)
	}
	vl.object(o, t, "")
	if len(vl.errs) == 0 {
		return nil
	}
	return vl.errs
}

type validator struct {
	cfg  validateConfig
	errs ValidationErrors
}

func (vl *validator) errf(path, format string, args ...any) {
	vl.errs = append(vl.errs, &ValidationError{
		Path: path,
		Msg:  fmt.Sprintf(format, args...),
	})
}

// walk validates one value against the Go type it is going to be decoded into.
func (vl *validator) walk(o *Object, t reflect.Type, path string) {
	if t == objectPointerTyp {
		return // *Object accepts anything
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
		if t == objectPointerTyp {
			return
		}
	}
	if o.IsNull() {
		return
	}
	switch {
	case t.Kind() == reflect.Interface:
		return
	case t == durationType, isTextUnmarshaler(t):
		vl.scalar(o, t, path)
	case t.Kind() == reflect.Struct:
		vl.object(o, t, path)
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		vl.scalar(o, t, path)
	case t.Kind() == reflect.Slice, t.Kind() == reflect.Array:
		vl.list(o, t, path)
	case t.Kind() == reflect.Map:
		vl.mapping(o, t, path)
	default:
		vl.scalar(o, t, path)
	}
}

func isTextUnmarshaler(t reflect.Type) bool {
	return t != durationType && reflect.PointerTo(t).Implements(textUnmarshalerType)
}

func (vl *validator) object(o *Object, t reflect.Type, path string) {
	if o.Type() != TypeObject {
		vl.errf(path, "expected an object, got %s", o.Type())
		return
	}
	known := make(map[string]bool)
	vl.fields(o, t, path, known)
	if !vl.cfg.rejectUnknown {
		return
	}
	for _, e := range o.Elems() {
		if !known[strings.ToLower(e.key)] {
			vl.errf(join(path, e.key), "unknown key")
		}
	}
}

// fields validates the fields of one struct type against an object. Inline
// fields are folded into the same object, so their names count as known here.
func (vl *validator) fields(o *Object, t reflect.Type, path string, known map[string]bool) {
	for _, f := range flatFields(t) {
		ft := fieldTypeByIndex(t, f.index)
		known[strings.ToLower(f.name)] = true

		child := lookupInsensitive(o, f.name)
		p := join(path, f.name)
		if f.rules.err != "" {
			// A malformed tag is a problem of the field itself, so report it
			// whether or not the document mentions the key.
			vl.errf(p, "%s", f.rules.err)
			continue
		}
		vl.checkDefault(f.rules, ft, p)
		if child == nil {
			if f.rules.required {
				vl.errf(p, "is required")
			}
			continue
		}
		vl.check(child, f.rules, ft, p)
		vl.walk(child, ft, p)
	}
}

// checkDefault reports a default= value that the constraints of its own
// field reject, or that could not be decoded into the field at all. The
// contradiction is a property of the struct rather than of the document, so
// it is reported whether or not the document supplies the key, and a test
// finds it instead of whatever reads the decoded value.
func (vl *validator) checkDefault(r rules, t reflect.Type, path string) {
	if r.defValue == nil {
		return
	}
	sub := &validator{cfg: vl.cfg}
	sub.check(r.defValue, r, t, path)
	sub.walk(r.defValue, t, path)
	for _, e := range sub.errs {
		vl.errf(e.Path, "invalid default: %s", e.Msg)
	}
}

// lookupInsensitive mirrors how the decoder matches keys to fields.
func lookupInsensitive(o *Object, name string) *Object {
	if v := o.Get(name); v != nil {
		return v
	}
	for _, e := range o.Elems() {
		if strings.EqualFold(e.key, name) {
			return e
		}
	}
	return nil
}

func (vl *validator) list(o *Object, t reflect.Type, path string) {
	// A scalar is accepted where a list is expected, the way the decoder
	// treats an implicit array of one element.
	if o.Type() != TypeArray {
		vl.walk(o, t.Elem(), path)
		return
	}
	for i, e := range o.Elems() {
		vl.walk(e, t.Elem(), fmt.Sprintf("%s[%d]", path, i))
	}
}

func (vl *validator) mapping(o *Object, t reflect.Type, path string) {
	if o.Type() != TypeObject {
		vl.errf(path, "expected an object, got %s", o.Type())
		return
	}
	for _, e := range o.Elems() {
		vl.walk(e, t.Elem(), join(path, e.key))
	}
}

// scalar checks that a value can become the Go type it is destined for.
func (vl *validator) scalar(o *Object, t reflect.Type, path string) {
	if o.IsContainer() {
		vl.errf(path, "expected %s, got %s", describe(t), o.Type())
		return
	}
	if t == durationType {
		if _, ok := durationSeconds(o); !ok {
			vl.errf(path, "expected a duration, got %q", o.ToString())
		}
		return
	}
	if isTextUnmarshaler(t) {
		return
	}
	switch t.Kind() {
	case reflect.Bool:
		if _, ok := o.BoolOK(); ok {
			return
		}
		if s, ok := o.StrOK(); ok {
			if _, ok := parseBool(s); ok {
				return
			}
		}
		vl.errf(path, "expected a boolean, got %q", o.ToString())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := numeric(o)
		if !ok {
			vl.errf(path, "expected an integer, got %q", o.ToString())
			return
		}
		if n != float64(int64(n)) {
			vl.errf(path, "expected an integer, got %s", o.ToString())
			return
		}
		if n < 0 && t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
			vl.errf(path, "must not be negative")
		}
	case reflect.Float32, reflect.Float64:
		if _, ok := numeric(o); !ok {
			vl.errf(path, "expected a number, got %q", o.ToString())
		}
	}
}

func describe(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "a boolean"
	case reflect.String:
		return "a string"
	case reflect.Struct, reflect.Map:
		return "an object"
	}
	return "a " + t.Kind().String()
}

// numeric returns the numeric value of an object, accepting the numeric
// strings the decoder accepts as well.
func numeric(o *Object) (float64, bool) {
	if f, ok := o.FloatOK(); ok {
		return f, true
	}
	if s, ok := o.StrOK(); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// durationSeconds decodes the forms a time.Duration field accepts.
func durationSeconds(o *Object) (float64, bool) {
	switch o.Type() {
	case TypeTime, TypeFloat, TypeInt:
		return o.Float(), true
	case TypeString:
		s := strings.TrimSpace(o.Str())
		if n := parseNumber(s, true); n != nil {
			return n.Float(), true
		}
		if d, err := time.ParseDuration(s); err == nil {
			return d.Seconds(), true
		}
	}
	return 0, false
}

// constraint kinds decide what min, max and len mean for a field.
const (
	kindOther = iota
	kindNumber
	kindDuration
	kindString
	kindList
)

func constraintKind(t reflect.Type) int {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == durationType {
		return kindDuration
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return kindNumber
	case reflect.String:
		return kindString
	case reflect.Slice, reflect.Array, reflect.Map:
		return kindList
	}
	return kindOther
}

// check applies the constraints of one field to the value found for it.
//
// min, max, len and unique describe the field as a whole: on a list they
// bound or examine the collection. enum, pattern and multipleof describe a
// single value, so on a list they are applied to each element in turn.
func (vl *validator) check(o *Object, r rules, t reflect.Type, path string) {
	if r.empty() {
		return
	}
	if r.err != "" {
		vl.errf(path, "%s", r.err)
		return
	}
	kind := constraintKind(t)

	if r.min != "" || r.max != "" || r.length != "" {
		vl.checkSize(o, r, kind, path)
	}
	if r.unique && kind == kindList && o.Type() == TypeArray {
		seen := make(map[string]bool, o.Len())
		for _, e := range o.Elems() {
			s := e.ToString()
			if seen[s] {
				vl.errf(path, "must not contain the duplicate value %q", s)
				break
			}
			seen[s] = true
		}
	}

	if r.multipleOf == "" && r.pattern == nil && len(r.enum) == 0 {
		return
	}
	if kind != kindList {
		vl.checkValue(o, r, kind, path)
		return
	}
	elemKind := constraintKind(elemType(t))
	for i, e := range o.Elems() {
		vl.checkValue(e, r, elemKind, elemPath(o, path, i, e))
	}
	if !o.IsContainer() {
		// A scalar stands for a list of one, the way the decoder reads it.
		vl.checkValue(o, r, elemKind, path)
	}
}

// checkValue applies the constraints that describe a single value.
func (vl *validator) checkValue(o *Object, r rules, kind int, path string) {
	if r.multipleOf != "" {
		vl.checkMultiple(o, r, kind, path)
	}
	if r.pattern != nil {
		if s, ok := o.StrOK(); !ok {
			vl.errf(path, "pattern applies to strings, not to %s", o.Type())
		} else if !r.pattern.MatchString(s) {
			vl.errf(path, "must match %s", r.pattern)
		}
	}
	if len(r.enum) > 0 {
		vl.checkEnum(o, r, kind, path)
	}
}

// elemType returns the type of the elements of a list, or t itself when t is
// not one.
func elemType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return t.Elem()
	}
	return t
}

// elemPath names one element of a list: by index in an array and by key in a
// map.
func elemPath(container *Object, path string, i int, e *Object) string {
	if container.Type() == TypeObject {
		return join(path, e.Key())
	}
	return fmt.Sprintf("%s[%d]", path, i)
}

// size returns the quantity min, max and len constrain, plus how to describe
// it in an error message.
func size(o *Object, kind int) (have float64, unit string, ok bool) {
	switch kind {
	case kindDuration:
		v, ok := durationSeconds(o)
		return v, "", ok
	case kindNumber:
		v, ok := numeric(o)
		return v, "", ok
	case kindString:
		s, ok := o.StrOK()
		return float64(utf8.RuneCountInString(s)), " characters", ok
	case kindList:
		if o.Type() != TypeArray && o.Type() != TypeObject {
			return 1, " items", true // a scalar counts as one element
		}
		return float64(o.Len()), " items", true
	}
	return 0, "", false
}

func (vl *validator) checkSize(o *Object, r rules, kind int, path string) {
	have, unit, ok := size(o, kind)
	if !ok {
		return // the type error is reported by scalar
	}
	for _, c := range []struct {
		raw string
		cmp func(have, want float64) bool
		msg string
	}{
		{r.length, func(h, w float64) bool { return h != w }, "must be exactly"},
		{r.min, func(h, w float64) bool { return h < w }, "must be at least"},
		{r.max, func(h, w float64) bool { return h > w }, "must be at most"},
	} {
		if c.raw == "" {
			continue
		}
		want, err := bound(c.raw, kind)
		if err != nil {
			vl.errf(path, "%s", err)
			continue
		}
		if c.cmp(have, want) {
			vl.errf(path, "%s %s%s, got %s%s",
				c.msg, format(want, kind), unit, format(have, kind), unit)
		}
	}
}

func (vl *validator) checkMultiple(o *Object, r rules, kind int, path string) {
	if kind != kindNumber && kind != kindDuration {
		vl.errf(path, "multipleof applies to numbers")
		return
	}
	have, _, ok := size(o, kind)
	if !ok {
		return
	}
	want, err := bound(r.multipleOf, kind)
	if err != nil {
		vl.errf(path, "%s", err)
		return
	}
	if want == 0 {
		vl.errf(path, "multipleof must not be zero")
		return
	}
	// Floating point leaves a remainder just under the divisor as readily
	// as just over zero, so both ends count as an exact multiple.
	rem := math.Abs(math.Mod(have, want))
	tol := 1e-9 * math.Max(math.Abs(have), math.Abs(want))
	if rem > tol && math.Abs(rem-math.Abs(want)) > tol {
		vl.errf(path, "must be a multiple of %s", format(want, kind))
	}
}

func (vl *validator) checkEnum(o *Object, r rules, kind int, path string) {
	have := o.ToString()
	for _, want := range r.enum {
		if have == want {
			return
		}
		if kind == kindNumber || kind == kindDuration {
			if w, err := bound(want, kind); err == nil {
				if h, _, ok := size(o, kind); ok && h == w {
					return
				}
			}
		}
	}
	vl.errf(path, "must be one of %s, got %q", strings.Join(r.enum, ", "), have)
}

// bound parses a constraint value written in a struct tag.
func bound(s string, kind int) (float64, error) {
	if kind == kindDuration {
		if n := parseNumber(s, true); n != nil {
			return n.Float(), nil
		}
		if d, err := time.ParseDuration(s); err == nil {
			return d.Seconds(), nil
		}
		return 0, fmt.Errorf("%q in the struct tag is not a duration", s)
	}
	// Accept the size suffixes a document may use, so that a byte count can
	// be bounded with max=64mb rather than with 67108864.
	if n := parseNumber(s, false); n != nil {
		return n.Float(), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q in the struct tag is not a number", s)
	}
	return v, nil
}

// format renders a bound the way it was most likely written.
func format(v float64, kind int) string {
	if kind == kindDuration {
		return time.Duration(v * float64(time.Second)).String()
	}
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
