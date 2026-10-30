package ucl

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"
)

// FromInterface converts a plain Go value into a UCL object tree. A nil value
// becomes the null object, and a *[Object] is returned as it is rather than
// copied, so the result may share a subtree with the input.
//
// Structs are converted field by field, honouring "ucl" struct tags, falling
// back to "json" tags and finally to the field name. The omitempty option
// drops a field holding the zero value of its type, and inline folds a nested
// struct into the enclosing object, where [Object.Decode] and [Validate] then
// look for it. The package documentation describes the
// tag under Struct tags; the constraint options [Validate] reads have no
// effect here.
//
// A time.Duration becomes a time value, a []byte becomes a string carrying
// [FlagBinary], and a value implementing encoding.TextMarshaler becomes its
// text. Map keys may be strings, integers or text marshalers, and the fields
// are written in sorted key order so that the output is stable.
func FromInterface(v any) (*Object, error) {
	if v == nil {
		return NewNull(), nil
	}
	if o, ok := v.(*Object); ok {
		return o, nil
	}
	return valueToObject(reflect.ValueOf(v))
}

// Marshal converts a Go value into a UCL document in the given format. It is
// [FromInterface] followed by [Object.Emit], so every [Format] may be used,
// including [MsgPack], whose output is binary.
func Marshal(v any, f Format) ([]byte, error) {
	o, err := FromInterface(v)
	if err != nil {
		return nil, err
	}
	return o.Emit(f)
}

var (
	durationType     = reflect.TypeOf(time.Duration(0))
	textMarshaler    = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	objectPointerTyp = reflect.TypeOf((*Object)(nil))
)

func valueToObject(v reflect.Value) (*Object, error) {
	if !v.IsValid() {
		return NewNull(), nil
	}
	if v.Type() == objectPointerTyp {
		if v.IsNil() {
			return NewNull(), nil
		}
		return v.Interface().(*Object), nil
	}
	if v.Type() == durationType {
		return NewTime(time.Duration(v.Int())), nil
	}
	if v.Type().Implements(textMarshaler) {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return NewNull(), nil
		}
		b, err := v.Interface().(encoding.TextMarshaler).MarshalText()
		if err != nil {
			return nil, err
		}
		return NewString(string(b)), nil
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return NewNull(), nil
		}
		return valueToObject(v.Elem())
	case reflect.Bool:
		return NewBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewInt(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return NewInt(int64(v.Uint())), nil
	case reflect.Float32, reflect.Float64:
		return NewFloat(v.Float()), nil
	case reflect.String:
		return NewString(v.String()), nil
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			o := NewString(string(v.Bytes()))
			o.flags |= FlagBinary
			return o, nil
		}
		fallthrough
	case reflect.Array:
		arr := NewArray()
		for i := 0; i < v.Len(); i++ {
			e, err := valueToObject(v.Index(i))
			if err != nil {
				return nil, err
			}
			arr.AppendElem(e)
		}
		return arr, nil
	case reflect.Map:
		return mapToObject(v)
	case reflect.Struct:
		return structToObject(v)
	}
	return nil, fmt.Errorf("ucl: cannot convert %s to an object", v.Type())
}

func mapToObject(v reflect.Value) (*Object, error) {
	obj := NewObject()
	keys := v.MapKeys()
	names := make([]string, len(keys))
	for i, k := range keys {
		name, err := mapKeyString(k)
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return names[order[a]] < names[order[b]] })
	for _, i := range order {
		e, err := valueToObject(v.MapIndex(keys[i]))
		if err != nil {
			return nil, err
		}
		obj.Set(names[i], e)
	}
	return obj, nil
}

func mapKeyString(k reflect.Value) (string, error) {
	// Checked before the kind, so that a named string type with a MarshalText
	// method is written the same way as a key as it would be as a value.
	// encoding/json orders these the same way.
	if k.Type().Implements(textMarshaler) {
		b, err := k.Interface().(encoding.TextMarshaler).MarshalText()
		return string(b), err
	}
	switch k.Kind() {
	case reflect.String:
		return k.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	return "", fmt.Errorf("ucl: unsupported map key type %s", k.Type())
}

func structToObject(v reflect.Value) (*Object, error) {
	obj := NewObject()
	for _, f := range structFields(v.Type()) {
		fv := fieldByIndex(v, f.index, false)
		if !fv.IsValid() {
			continue
		}
		if f.omitEmpty && isEmptyValue(fv) {
			continue
		}
		if f.inline {
			sub, err := valueToObject(fv)
			if err != nil {
				return nil, err
			}
			if sub.Type() == TypeObject {
				if err := obj.Merge(sub, DuplicateRewrite); err != nil {
					return nil, err
				}
				continue
			}
		}
		e, err := valueToObject(fv)
		if err != nil {
			return nil, err
		}
		obj.Set(f.name, e)
	}
	return obj, nil
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}
