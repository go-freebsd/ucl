package ucl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Type describes the type of a UCL object. The set of types mirrors the
// UCL_* types of libucl.
type Type uint8

const (
	// TypeObject is a key/value container.
	TypeObject Type = iota
	// TypeArray is an ordered list of objects.
	TypeArray
	// TypeInt is a signed 64 bit integer.
	TypeInt
	// TypeFloat is a double precision floating point number.
	TypeFloat
	// TypeString is a UTF-8 string.
	TypeString
	// TypeBoolean is a boolean value.
	TypeBoolean
	// TypeTime is a time interval, stored as seconds.
	TypeTime
	// TypeUserData carries an arbitrary Go value and is never produced by
	// the parser.
	TypeUserData
	// TypeNull is the explicit null value.
	TypeNull
)

var typeNames = [...]string{
	TypeObject:   "object",
	TypeArray:    "array",
	TypeInt:      "int",
	TypeFloat:    "float",
	TypeString:   "string",
	TypeBoolean:  "boolean",
	TypeTime:     "time",
	TypeUserData: "userdata",
	TypeNull:     "null",
}

// String implements fmt.Stringer.
func (t Type) String() string {
	if int(t) < len(typeNames) {
		return typeNames[t]
	}
	return "unknown"
}

// Flags carry additional information about how an object was defined in the
// source document.
type Flags uint16

const (
	// FlagMultiValue marks an array that was created implicitly by using the
	// same key more than once inside one object.
	FlagMultiValue Flags = 1 << iota
	// FlagMultiline marks a string that was defined using heredoc syntax.
	FlagMultiline
	// FlagSingleQuoted marks a string that was single quoted in the source.
	FlagSingleQuoted
	// FlagBinary marks a string that holds arbitrary binary data.
	FlagBinary
)

// MaxPriority is the highest priority an object may carry. libucl stores the
// priority in four bits. [Object.SetPriority] and [WithPriority] clamp a
// larger value to this one, while the .priority macro and the priority
// parameter of .include report an error for it.
const MaxPriority = 15

// Duplicate selects the strategy used when a key is inserted into an object
// that already contains it.
type Duplicate uint8

const (
	// DuplicateAppend turns repeated keys of equal priority into an implicit
	// array. This is the default and what plain UCL documents rely on.
	DuplicateAppend Duplicate = iota
	// DuplicateMerge merges objects and arrays recursively and overwrites
	// everything else.
	DuplicateMerge
	// DuplicateRewrite replaces the previous value.
	DuplicateRewrite
	// DuplicateError reports an error on a repeated key.
	DuplicateError
)

// String implements fmt.Stringer.
func (d Duplicate) String() string {
	switch d {
	case DuplicateAppend:
		return "append"
	case DuplicateMerge:
		return "merge"
	case DuplicateRewrite:
		return "rewrite"
	case DuplicateError:
		return "error"
	}
	return "unknown"
}

// Object is a node of a UCL document. The zero value is not usable, objects
// are created by the New* constructors or by a Parser.
//
// All read accessors tolerate a nil receiver, which makes chained lookups such
// as cfg.Get("a").Get("b").StringOr("") safe to write.
type Object struct {
	typ      Type
	key      string
	flags    Flags
	priority uint8

	iv int64
	dv float64
	bv bool
	sv string
	ud any

	// elems holds array elements as well as object fields, both in insertion
	// order. index maps a field name to its position in elems and is only
	// used for TypeObject.
	elems []*Object
	index map[string]int
}

// NewObject returns an empty object.
func NewObject() *Object {
	return &Object{typ: TypeObject, index: make(map[string]int)}
}

// NewArray returns an array holding the given values.
func NewArray(vals ...*Object) *Object {
	return &Object{typ: TypeArray, elems: append([]*Object(nil), vals...)}
}

// NewString returns a string object.
func NewString(s string) *Object { return &Object{typ: TypeString, sv: s} }

// NewInt returns an integer object.
func NewInt(i int64) *Object { return &Object{typ: TypeInt, iv: i} }

// NewFloat returns a floating point object.
func NewFloat(f float64) *Object { return &Object{typ: TypeFloat, dv: f} }

// NewBool returns a boolean object.
func NewBool(b bool) *Object { return &Object{typ: TypeBoolean, bv: b} }

// NewTime returns a time object. UCL stores time intervals as seconds.
func NewTime(d time.Duration) *Object {
	return &Object{typ: TypeTime, dv: d.Seconds()}
}

// NewSeconds returns a time object from a number of seconds.
func NewSeconds(sec float64) *Object { return &Object{typ: TypeTime, dv: sec} }

// NewNull returns the null object.
func NewNull() *Object { return &Object{typ: TypeNull} }

// NewUserData wraps an arbitrary Go value. User data is emitted as null by
// every emitter but survives lookups.
func NewUserData(v any) *Object { return &Object{typ: TypeUserData, ud: v} }

// Type returns the type of the object. A nil object reports TypeNull.
func (o *Object) Type() Type {
	if o == nil {
		return TypeNull
	}
	return o.typ
}

// Key returns the name under which the object is stored in its parent, if any.
func (o *Object) Key() string {
	if o == nil {
		return ""
	}
	return o.key
}

// Flags returns the object flags.
func (o *Object) Flags() Flags {
	if o == nil {
		return 0
	}
	return o.flags
}

// Priority returns the priority of the object.
func (o *Object) Priority() uint8 {
	if o == nil {
		return 0
	}
	return o.priority
}

// SetPriority sets the priority of the object. Priorities above MaxPriority
// are clamped.
func (o *Object) SetPriority(p uint8) {
	if o == nil {
		return
	}
	if p > MaxPriority {
		p = MaxPriority
	}
	o.priority = p
}

// IsNull reports whether the object is nil or the null value.
func (o *Object) IsNull() bool { return o == nil || o.typ == TypeNull }

// IsContainer reports whether the object is an object or an array.
func (o *Object) IsContainer() bool {
	return o != nil && (o.typ == TypeObject || o.typ == TypeArray)
}

// Len returns the number of fields of an object or the number of elements of
// an array. A scalar reports one and a nil object reports zero, but neither
// can be indexed with At.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	switch o.typ {
	case TypeObject, TypeArray:
		return len(o.elems)
	}
	return 1
}

// Keys returns the field names of an object in insertion order.
func (o *Object) Keys() []string {
	if o == nil || o.typ != TypeObject {
		return nil
	}
	keys := make([]string, 0, len(o.elems))
	for _, e := range o.elems {
		keys = append(keys, e.key)
	}
	return keys
}

// Get returns the value stored under key, or nil. Repeated keys are reported
// as a single implicit array, use Values to obtain the individual values.
func (o *Object) Get(key string) *Object {
	if o == nil || o.typ != TypeObject {
		return nil
	}
	if i, ok := o.index[key]; ok {
		return o.elems[i]
	}
	return nil
}

// Has reports whether the object contains key.
func (o *Object) Has(key string) bool { return o.Get(key) != nil }

// Values returns every value stored under key. An implicit array created by a
// repeated key is flattened, an explicit array is returned as a single value.
func (o *Object) Values(key string) []*Object {
	v := o.Get(key)
	if v == nil {
		return nil
	}
	if v.typ == TypeArray && v.flags&FlagMultiValue != 0 {
		return append([]*Object(nil), v.elems...)
	}
	return []*Object{v}
}

// At returns the i-th element of an array or the i-th field of an object.
// Scalars and out of range indices yield nil.
func (o *Object) At(i int) *Object {
	if o == nil || i < 0 || i >= len(o.elems) {
		return nil
	}
	return o.elems[i]
}

// Elems returns the elements of an array or the fields of an object in
// document order. Unlike [Object.Keys] and [Object.Values] it exposes the
// object's own slice rather than a copy, so the result must not be modified.
func (o *Object) Elems() []*Object {
	if o == nil {
		return nil
	}
	return o.elems
}

// Each calls fn for every field of an object or every element of an array
// until fn returns false. For arrays the key is empty.
func (o *Object) Each(fn func(key string, val *Object) bool) {
	if o == nil {
		return
	}
	for _, e := range o.elems {
		if !fn(e.key, e) {
			return
		}
	}
}

// Lookup resolves a dotted path such as "section.sub.key". Numeric components
// index into arrays and implicit arrays. It returns nil if any component is
// missing.
func (o *Object) Lookup(path string) *Object {
	cur := o
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		if cur == nil {
			return nil
		}
		switch cur.typ {
		case TypeObject:
			cur = cur.Get(part)
		case TypeArray:
			i, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			cur = cur.At(i)
		default:
			return nil
		}
	}
	return cur
}

// AppendElem appends a value to an array.
func (o *Object) AppendElem(v *Object) {
	if o == nil || v == nil || o.typ != TypeArray {
		return
	}
	o.elems = append(o.elems, v)
}

// Set stores v under key, replacing any previous value.
func (o *Object) Set(key string, v *Object) {
	_ = o.Insert(key, v, DuplicateRewrite)
}

// Add stores v under key using the default append strategy, which turns
// repeated keys into an implicit array.
func (o *Object) Add(key string, v *Object) {
	_ = o.Insert(key, v, DuplicateAppend)
}

// Insert stores v under key using the given duplicate strategy. It returns an
// error when the receiver is not an object, and when the strategy is
// [DuplicateError] and the key is already present.
func (o *Object) Insert(key string, v *Object, strategy Duplicate) error {
	if o == nil || v == nil {
		return nil
	}
	if o.typ != TypeObject {
		return fmt.Errorf("ucl: cannot insert %q into a %s", key, o.typ)
	}
	if o.index == nil {
		o.index = make(map[string]int)
	}
	v.key = key
	i, ok := o.index[key]
	if !ok {
		o.index[key] = len(o.elems)
		o.elems = append(o.elems, v)
		return nil
	}
	old := o.elems[i]
	switch strategy {
	case DuplicateError:
		return fmt.Errorf("ucl: duplicate key %q", key)
	case DuplicateRewrite:
		o.elems[i] = v
	case DuplicateMerge:
		o.elems[i] = mergeValues(old, v)
	default: // DuplicateAppend
		switch {
		case old.priority > v.priority:
			// The existing value wins, drop the new one.
		case old.priority < v.priority:
			o.elems[i] = v
		default:
			o.elems[i] = appendValue(old, v)
		}
	}
	o.elems[i].key = key
	return nil
}

// appendValue builds or extends an implicit array out of two values.
func appendValue(old, v *Object) *Object {
	if old.typ == TypeArray && old.flags&FlagMultiValue != 0 {
		old.elems = append(old.elems, v)
		return old
	}
	arr := NewArray(old, v)
	arr.flags |= FlagMultiValue
	arr.priority = old.priority
	return arr
}

// mergeValues implements the merge duplicate strategy.
func mergeValues(old, v *Object) *Object {
	switch {
	case old.typ == TypeObject && v.typ == TypeObject:
		old.Merge(v, DuplicateMerge)
		return old
	case old.typ == TypeArray && v.typ == TypeArray:
		old.elems = append(old.elems, v.elems...)
		return old
	case old.typ == TypeArray:
		old.elems = append(old.elems, v)
		return old
	default:
		return v
	}
}

// Merge inserts every field of src into o using the given strategy. It returns
// an error when either side is not an object.
//
// Nothing is copied: o ends up holding the same child objects as src, so a
// later change to one tree is visible in the other. [DuplicateMerge] and
// [DuplicateAppend] also change the values already present in o in place. Pass
// src.Copy() when src has to stay as it is.
func (o *Object) Merge(src *Object, strategy Duplicate) error {
	if o == nil || src == nil {
		return nil
	}
	if o.typ != TypeObject || src.typ != TypeObject {
		return fmt.Errorf("ucl: cannot merge %s into %s", src.typ, o.typ)
	}
	for _, e := range src.elems {
		if err := o.Insert(e.key, e, strategy); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes key from an object and reports whether it was present.
func (o *Object) Delete(key string) bool {
	if o == nil || o.typ != TypeObject {
		return false
	}
	i, ok := o.index[key]
	if !ok {
		return false
	}
	o.elems = append(o.elems[:i], o.elems[i+1:]...)
	delete(o.index, key)
	for k, pos := range o.index {
		if pos > i {
			o.index[k] = pos - 1
		}
	}
	return true
}

// Copy returns a deep copy of the object. The payload of a [TypeUserData]
// object is copied by reference, everything else is independent of the
// original.
func (o *Object) Copy() *Object {
	if o == nil {
		return nil
	}
	c := *o
	c.elems = nil
	c.index = nil
	if o.typ == TypeObject {
		c.index = make(map[string]int, len(o.index))
	}
	for _, e := range o.elems {
		ec := e.Copy()
		if o.typ == TypeObject {
			c.index[ec.key] = len(c.elems)
		}
		c.elems = append(c.elems, ec)
	}
	return &c
}

// Int returns the integer value of the object, converting from the other
// numeric types. Non numeric objects yield zero.
func (o *Object) Int() int64 { v, _ := o.IntOK(); return v }

// IntOK is Int with a flag reporting whether the conversion was possible.
func (o *Object) IntOK() (int64, bool) {
	if o == nil {
		return 0, false
	}
	switch o.typ {
	case TypeInt:
		return o.iv, true
	case TypeFloat, TypeTime:
		return int64(o.dv), true
	case TypeBoolean:
		if o.bv {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// IntOr returns the integer value or def if the object is not numeric.
func (o *Object) IntOr(def int64) int64 {
	if v, ok := o.IntOK(); ok {
		return v
	}
	return def
}

// Float returns the floating point value of the object.
func (o *Object) Float() float64 { v, _ := o.FloatOK(); return v }

// FloatOK is Float with a flag reporting whether the conversion was possible.
func (o *Object) FloatOK() (float64, bool) {
	if o == nil {
		return 0, false
	}
	switch o.typ {
	case TypeFloat, TypeTime:
		return o.dv, true
	case TypeInt:
		return float64(o.iv), true
	case TypeBoolean:
		if o.bv {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// FloatOr returns the float value or def if the object is not numeric.
func (o *Object) FloatOr(def float64) float64 {
	if v, ok := o.FloatOK(); ok {
		return v
	}
	return def
}

// Bool returns the boolean value of the object. Numbers are true when they
// are not zero.
func (o *Object) Bool() bool { v, _ := o.BoolOK(); return v }

// BoolOK is Bool with a flag reporting whether the conversion was possible.
func (o *Object) BoolOK() (bool, bool) {
	if o == nil {
		return false, false
	}
	switch o.typ {
	case TypeBoolean:
		return o.bv, true
	case TypeInt:
		return o.iv != 0, true
	case TypeFloat, TypeTime:
		return o.dv != 0, true
	}
	return false, false
}

// BoolOr returns the boolean value or def if the object is not convertible.
func (o *Object) BoolOr(def bool) bool {
	if v, ok := o.BoolOK(); ok {
		return v
	}
	return def
}

// Str returns the string value of a string object.
func (o *Object) Str() string { v, _ := o.StrOK(); return v }

// StrOK is Str with a flag reporting whether the object is a string.
func (o *Object) StrOK() (string, bool) {
	if o == nil || o.typ != TypeString {
		return "", false
	}
	return o.sv, true
}

// StrOr returns the string value or def if the object is not a string.
func (o *Object) StrOr(def string) string {
	if v, ok := o.StrOK(); ok {
		return v
	}
	return def
}

// Duration returns a time value as a time.Duration. Plain integers and floats
// are read as a number of seconds, everything else yields zero.
func (o *Object) Duration() time.Duration {
	v, _ := o.FloatOK()
	return time.Duration(v * float64(time.Second))
}

// UserData returns the Go value of a user data object.
func (o *Object) UserData() any {
	if o == nil {
		return nil
	}
	return o.ud
}

// ToString renders any scalar as a string, the way ucl_object_tostring_forced
// does. Containers are rendered as their compact JSON representation.
func (o *Object) ToString() string {
	if o == nil {
		return ""
	}
	switch o.typ {
	case TypeString:
		return o.sv
	case TypeInt:
		return strconv.FormatInt(o.iv, 10)
	case TypeFloat, TypeTime:
		return formatFloat(o.dv)
	case TypeBoolean:
		return strconv.FormatBool(o.bv)
	case TypeNull:
		return "null"
	case TypeUserData:
		return fmt.Sprint(o.ud)
	}
	b, _ := o.Emit(JSONCompact) // JSONCompact never fails
	return string(b)
}

// Interface converts the object into plain Go values: map[string]any for
// objects, []any for arrays, int64, float64, string, bool and nil for the
// scalars. Time objects become time.Duration.
func (o *Object) Interface() any {
	if o == nil {
		return nil
	}
	switch o.typ {
	case TypeObject:
		m := make(map[string]any, len(o.elems))
		for _, e := range o.elems {
			m[e.key] = e.Interface()
		}
		return m
	case TypeArray:
		s := make([]any, 0, len(o.elems))
		for _, e := range o.elems {
			s = append(s, e.Interface())
		}
		return s
	case TypeInt:
		return o.iv
	case TypeFloat:
		return o.dv
	case TypeTime:
		return time.Duration(o.dv * float64(time.Second))
	case TypeString:
		return o.sv
	case TypeBoolean:
		return o.bv
	case TypeUserData:
		return o.ud
	}
	return nil
}

// String implements fmt.Stringer and renders the object in UCL configuration
// syntax.
func (o *Object) String() string {
	b, _ := o.Emit(Config) // Config never fails
	return string(b)
}

// formatFloat renders a float the way libucl does: no exponent for ordinary
// values and always at least one fractional digit.
func formatFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "null"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
