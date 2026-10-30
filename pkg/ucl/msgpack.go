package ucl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// emitMsgPack writes the object tree using the msgpack encoding, the same one
// libucl produces for UCL_EMIT_MSGPACK.
func emitMsgPack(b *bytes.Buffer, o *Object) error {
	switch o.Type() {
	case TypeObject:
		writeMsgPackLen(b, len(o.elems), 0x80, 0xde, 0xdf)
		for _, e := range o.elems {
			writeMsgPackString(b, e.key)
			if err := emitMsgPack(b, e); err != nil {
				return err
			}
		}
	case TypeArray:
		writeMsgPackLen(b, len(o.elems), 0x90, 0xdc, 0xdd)
		for _, e := range o.elems {
			if err := emitMsgPack(b, e); err != nil {
				return err
			}
		}
	case TypeString:
		writeMsgPackString(b, o.sv)
	case TypeInt:
		writeMsgPackInt(b, o.iv)
	case TypeFloat, TypeTime:
		b.WriteByte(0xcb)
		binary.Write(b, binary.BigEndian, math.Float64bits(o.dv))
	case TypeBoolean:
		if o.bv {
			b.WriteByte(0xc3)
		} else {
			b.WriteByte(0xc2)
		}
	case TypeNull, TypeUserData:
		b.WriteByte(0xc0)
	default:
		return fmt.Errorf("ucl: cannot encode %s as msgpack", o.Type())
	}
	return nil
}

// writeMsgPackLen writes a length prefix using the fixed, 16 bit or 32 bit
// form depending on the size.
func writeMsgPackLen(b *bytes.Buffer, n int, fixed, w16, w32 byte) {
	switch {
	case n < 16:
		b.WriteByte(fixed | byte(n))
	case n <= math.MaxUint16:
		b.WriteByte(w16)
		binary.Write(b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(w32)
		binary.Write(b, binary.BigEndian, uint32(n))
	}
}

func writeMsgPackString(b *bytes.Buffer, s string) {
	switch n := len(s); {
	case n < 32:
		b.WriteByte(0xa0 | byte(n))
	case n <= math.MaxUint8:
		b.WriteByte(0xd9)
		b.WriteByte(byte(n))
	case n <= math.MaxUint16:
		b.WriteByte(0xda)
		binary.Write(b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(0xdb)
		binary.Write(b, binary.BigEndian, uint32(n))
	}
	b.WriteString(s)
}

func writeMsgPackInt(b *bytes.Buffer, v int64) {
	switch {
	case v >= 0 && v < 128:
		b.WriteByte(byte(v))
	case v < 0 && v >= -32:
		b.WriteByte(byte(0xe0 | (v + 32)))
	case v >= math.MinInt8 && v <= math.MaxInt8:
		b.WriteByte(0xd0)
		b.WriteByte(byte(int8(v)))
	case v >= math.MinInt16 && v <= math.MaxInt16:
		b.WriteByte(0xd1)
		binary.Write(b, binary.BigEndian, int16(v))
	case v >= math.MinInt32 && v <= math.MaxInt32:
		b.WriteByte(0xd2)
		binary.Write(b, binary.BigEndian, int32(v))
	default:
		b.WriteByte(0xd3)
		binary.Write(b, binary.BigEndian, v)
	}
}

// ParseMsgPack decodes a msgpack document into an object tree. It accepts the
// same subset libucl does, which excludes extension types. Only the first
// value is decoded; any bytes that follow it are ignored.
func ParseMsgPack(data []byte) (*Object, error) {
	d := &msgpackDecoder{data: data}
	return d.value()
}

type msgpackDecoder struct {
	data []byte
	pos  int
}

func (d *msgpackDecoder) need(n int) error {
	if d.pos+n > len(d.data) {
		return fmt.Errorf("ucl: truncated msgpack input at offset %d", d.pos)
	}
	return nil
}

func (d *msgpackDecoder) byteAt() (byte, error) {
	if err := d.need(1); err != nil {
		return 0, err
	}
	c := d.data[d.pos]
	d.pos++
	return c, nil
}

func (d *msgpackDecoder) take(n int) ([]byte, error) {
	if err := d.need(n); err != nil {
		return nil, err
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *msgpackDecoder) uint(size int) (uint64, error) {
	b, err := d.take(size)
	if err != nil {
		return 0, err
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v, nil
}

func (d *msgpackDecoder) value() (*Object, error) {
	c, err := d.byteAt()
	if err != nil {
		return nil, err
	}
	switch {
	case c <= 0x7f:
		return NewInt(int64(c)), nil
	case c >= 0xe0:
		return NewInt(int64(int8(c))), nil
	case c&0xf0 == 0x80:
		return d.mapValue(int(c & 0x0f))
	case c&0xf0 == 0x90:
		return d.arrayValue(int(c & 0x0f))
	case c&0xe0 == 0xa0:
		return d.stringValue(int(c & 0x1f))
	}
	switch c {
	case 0xc0:
		return NewNull(), nil
	case 0xc2:
		return NewBool(false), nil
	case 0xc3:
		return NewBool(true), nil
	case 0xc4, 0xc5, 0xc6: // bin 8, 16, 32
		n, err := d.uint(1 << (c - 0xc4))
		if err != nil {
			return nil, err
		}
		b, err := d.take(int(n))
		if err != nil {
			return nil, err
		}
		o := NewString(string(b))
		o.flags |= FlagBinary
		return o, nil
	case 0xca:
		v, err := d.uint(4)
		if err != nil {
			return nil, err
		}
		return NewFloat(float64(math.Float32frombits(uint32(v)))), nil
	case 0xcb:
		v, err := d.uint(8)
		if err != nil {
			return nil, err
		}
		return NewFloat(math.Float64frombits(v)), nil
	case 0xcc, 0xcd, 0xce, 0xcf: // uint 8, 16, 32, 64
		v, err := d.uint(1 << (c - 0xcc))
		if err != nil {
			return nil, err
		}
		if v > math.MaxInt64 {
			// UCL has no unsigned type. Keeping the magnitude as a float
			// loses precision, but wrapping it into a negative integer
			// would be wrong rather than merely approximate.
			return NewFloat(float64(v)), nil
		}
		return NewInt(int64(v)), nil
	case 0xd0, 0xd1, 0xd2, 0xd3: // int 8, 16, 32, 64
		size := 1 << (c - 0xd0)
		v, err := d.uint(size)
		if err != nil {
			return nil, err
		}
		shift := 64 - 8*size
		return NewInt(int64(v<<shift) >> shift), nil
	case 0xd9, 0xda, 0xdb: // str 8, 16, 32
		n, err := d.uint(1 << (c - 0xd9))
		if err != nil {
			return nil, err
		}
		return d.stringValue(int(n))
	case 0xdc, 0xdd: // array 16, 32
		n, err := d.uint(2 << (c - 0xdc))
		if err != nil {
			return nil, err
		}
		return d.arrayValue(int(n))
	case 0xde, 0xdf: // map 16, 32
		n, err := d.uint(2 << (c - 0xde))
		if err != nil {
			return nil, err
		}
		return d.mapValue(int(n))
	}
	return nil, fmt.Errorf("ucl: unsupported msgpack type 0x%02x at offset %d", c, d.pos-1)
}

func (d *msgpackDecoder) stringValue(n int) (*Object, error) {
	b, err := d.take(n)
	if err != nil {
		return nil, err
	}
	return NewString(string(b)), nil
}

func (d *msgpackDecoder) arrayValue(n int) (*Object, error) {
	arr := NewArray()
	for i := 0; i < n; i++ {
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		arr.AppendElem(v)
	}
	return arr, nil
}

func (d *msgpackDecoder) mapValue(n int) (*Object, error) {
	obj := NewObject()
	for i := 0; i < n; i++ {
		k, err := d.value()
		if err != nil {
			return nil, err
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		obj.Set(k.ToString(), v)
	}
	return obj, nil
}
