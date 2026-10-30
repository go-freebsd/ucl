package ucl

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Unmarshal parses a UCL document and stores the result in the value pointed
// to by v. Field names and tag options are those of [Object.Decode].
func Unmarshal(data []byte, v any, opts ...Option) error {
	o, err := Parse(data, opts...)
	if err != nil {
		return err
	}
	return o.Decode(v)
}

// UnmarshalFile reads and parses a file and stores the result in the value
// pointed to by v. Field names and tag options are those of [Object.Decode].
func UnmarshalFile(path string, v any, opts ...Option) error {
	o, err := ParseFile(path, opts...)
	if err != nil {
		return err
	}
	return o.Decode(v)
}

// Decode stores the object in the value pointed to by v, which must be a non
// nil pointer.
//
// Field names are taken from "ucl" struct tags, then from "json" tags, then
// from the field name itself, matched case insensitively with an exact match
// preferred. The package documentation describes the tag under Struct tags.
// A field tagged inline is filled from the keys of the enclosing object
// rather than from one named after the field, which is where [FromInterface]
// writes it. The omitempty option has no effect when decoding, and neither do
// the constraint options [Validate] reads.
//
// Values are converted to the type of the destination: numbers and numeric
// strings fill the numeric kinds, the UCL boolean spellings fill a bool, any
// scalar fills a string, a string fills a []byte, and an object fills a map or
// a struct. A scalar where a slice is expected becomes a one element slice,
// which is what makes an implicit array of a single value decode. A
// time.Duration accepts a UCL time value, a plain number of seconds, or a
// string time.ParseDuration understands. A field of type *[Object] receives
// the subtree itself, uncopied. A type whose pointer implements
// encoding.TextUnmarshaler is handed the value as [Object.ToString] renders
// it.
//
// Fields the document does not mention are left as they are, so one way to
// express a default is to fill the value in before decoding into it. The other
// is the default= tag option, which is applied to a field the document did not
// mention and the caller did not already set. Keys that no field matches are
// ignored; use [Validate] with [RejectUnknownKeys] to report them.
func (o *Object) Decode(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("ucl: Decode needs a non nil pointer, got %T", v)
	}
	return decodeValue(o, rv.Elem(), "")
}

// decodeError builds an error naming the offending path.
func decodeError(path string, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if path == "" {
		return fmt.Errorf("ucl: %s", msg)
	}
	return fmt.Errorf("ucl: %s: %s", path, msg)
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func decodeValue(o *Object, v reflect.Value, path string) error {
	if v.Type() == objectPointerTyp {
		v.Set(reflect.ValueOf(o))
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if o.IsNull() {
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return decodeValue(o, v.Elem(), path)
	}
	if v.CanAddr() {
		if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok &&
			v.Type() != durationType {
			return u.UnmarshalText([]byte(o.ToString()))
		}
	}
	if v.Type() == durationType {
		return decodeDuration(o, v, path)
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return decodeError(path, "cannot decode into %s", v.Type())
		}
		v.Set(reflect.ValueOf(o.Interface()))
		return nil
	case reflect.Bool:
		b, ok := o.BoolOK()
		if !ok {
			if s, isStr := o.StrOK(); isStr {
				if b, ok = parseBool(s); !ok {
					return decodeError(path, "cannot use %q as a boolean", s)
				}
			} else {
				return decodeError(path, "cannot use %s as a boolean", o.Type())
			}
		}
		v.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt(o, path)
		if err != nil {
			return err
		}
		if v.OverflowInt(n) {
			return decodeError(path, "%d overflows %s", n, v.Type())
		}
		v.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := toInt(o, path)
		if err != nil {
			return err
		}
		if n < 0 || v.OverflowUint(uint64(n)) {
			return decodeError(path, "%d does not fit into %s", n, v.Type())
		}
		v.SetUint(uint64(n))
		return nil
	case reflect.Float32, reflect.Float64:
		f, ok := o.FloatOK()
		if !ok {
			s, isStr := o.StrOK()
			if !isStr {
				return decodeError(path, "cannot use %s as a number", o.Type())
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return decodeError(path, "cannot use %q as a number", s)
			}
			f = parsed
		}
		v.SetFloat(f)
		return nil
	case reflect.String:
		if o.Type() == TypeObject || o.Type() == TypeArray {
			return decodeError(path, "cannot use %s as a string", o.Type())
		}
		v.SetString(o.ToString())
		return nil
	case reflect.Slice, reflect.Array:
		return decodeSlice(o, v, path)
	case reflect.Map:
		return decodeMap(o, v, path)
	case reflect.Struct:
		return decodeStruct(o, v, path)
	}
	return decodeError(path, "cannot decode into %s", v.Type())
}

func toInt(o *Object, path string) (int64, error) {
	if n, ok := o.IntOK(); ok {
		return n, nil
	}
	if s, ok := o.StrOK(); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 0, 64); err == nil {
			return n, nil
		}
		return 0, decodeError(path, "cannot use %q as an integer", s)
	}
	return 0, decodeError(path, "cannot use %s as an integer", o.Type())
}

// decodeDuration accepts a UCL time value, a plain number of seconds or a
// string understood by time.ParseDuration.
func decodeDuration(o *Object, v reflect.Value, path string) error {
	switch o.Type() {
	case TypeTime, TypeFloat, TypeInt:
		v.SetInt(int64(o.Duration()))
		return nil
	case TypeString:
		s := strings.TrimSpace(o.Str())
		if n := parseNumber(s, true); n != nil {
			v.SetInt(int64(n.Duration()))
			return nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return decodeError(path, "cannot use %q as a duration", s)
		}
		v.SetInt(int64(d))
		return nil
	}
	return decodeError(path, "cannot use %s as a duration", o.Type())
}

// decodeSlice fills a slice or array. A scalar is accepted as a one element
// list, which makes implicit arrays convenient to consume.
func decodeSlice(o *Object, v reflect.Value, path string) error {
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 &&
		o.Type() == TypeString {
		v.SetBytes([]byte(o.Str()))
		return nil
	}
	elems := o.Elems()
	if o.Type() != TypeArray {
		if o.IsNull() {
			if v.Kind() == reflect.Slice {
				v.Set(reflect.Zero(v.Type()))
			}
			return nil
		}
		elems = []*Object{o}
	}
	if v.Kind() == reflect.Array {
		if len(elems) > v.Len() {
			return decodeError(path, "%d values do not fit into %s", len(elems), v.Type())
		}
	} else {
		v.Set(reflect.MakeSlice(v.Type(), len(elems), len(elems)))
	}
	for i, e := range elems {
		if err := decodeValue(e, v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func decodeMap(o *Object, v reflect.Value, path string) error {
	if o.Type() != TypeObject {
		if o.IsNull() {
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		return decodeError(path, "cannot use %s as a map", o.Type())
	}
	kt := v.Type().Key()
	if kt.Kind() != reflect.String {
		return decodeError(path, "unsupported map key type %s", kt)
	}
	m := reflect.MakeMapWithSize(v.Type(), o.Len())
	for _, e := range o.Elems() {
		ev := reflect.New(v.Type().Elem()).Elem()
		if err := decodeValue(e, ev, join(path, e.key)); err != nil {
			return err
		}
		m.SetMapIndex(reflect.ValueOf(e.key).Convert(kt), ev)
	}
	v.Set(m)
	return nil
}

func decodeStruct(o *Object, v reflect.Value, path string) error {
	if o.Type() != TypeObject {
		if o.IsNull() {
			return nil
		}
		return decodeError(path, "cannot use %s as %s", o.Type(), v.Type())
	}
	fields := flatFields(v.Type())
	filled := make(map[string]bool, len(fields))
	for _, e := range o.Elems() {
		f := matchField(fields, e.key)
		if f == nil {
			continue
		}
		fv := fieldByIndex(v, f.index, true)
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}
		if err := decodeValue(e, fv, join(path, e.key)); err != nil {
			return err
		}
		filled[f.name] = true
	}
	return applyDefaults(fields, filled, v, path)
}

// applyDefaults fills in the default= options of the keys the document did
// not mention. A field the caller has already set is left alone, so a struct
// built with values of its own still wins.
func applyDefaults(fields []fieldInfo, filled map[string]bool, v reflect.Value, path string) error {
	for i := range fields {
		f := &fields[i]
		if f.rules.defValue == nil || filled[f.name] {
			continue
		}
		if f.rules.err != "" {
			return decodeError(join(path, f.name), "%s", f.rules.err)
		}
		fv := fieldByIndex(v, f.index, true)
		if !fv.IsValid() || !fv.CanSet() || !isEmptyValue(fv) {
			continue
		}
		if err := decodeValue(f.rules.defValue, fv, join(path, f.name)); err != nil {
			return err
		}
	}
	return nil
}

// matchField finds the field for a key, preferring an exact match over a case
// insensitive one.
func matchField(fields []fieldInfo, key string) *fieldInfo {
	for i := range fields {
		if fields[i].name == key {
			return &fields[i]
		}
	}
	for i := range fields {
		if strings.EqualFold(fields[i].name, key) {
			return &fields[i]
		}
	}
	return nil
}
