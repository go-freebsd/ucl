package ucl

import (
	"math"
	"strings"
	"testing"
)

// The round trip tests only prove that this package agrees with itself: a
// wrong tag byte would be wrong in the encoder and the decoder together, and
// the wide length forms are never emitted for the small values a test uses.
// These cases feed the decoder bytes written out by hand instead.
func TestMsgPackDecodeTags(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		typ  Type
		want string
	}{
		{"null", []byte{0xc0}, TypeNull, "null"},
		{"false", []byte{0xc2}, TypeBoolean, "false"},
		{"true", []byte{0xc3}, TypeBoolean, "true"},
		{"positive fixint", []byte{0x7f}, TypeInt, "127"},
		{"negative fixint", []byte{0xff}, TypeInt, "-1"},
		{"negative fixint min", []byte{0xe0}, TypeInt, "-32"},

		{"uint8", []byte{0xcc, 0xff}, TypeInt, "255"},
		{"uint16", []byte{0xcd, 0x01, 0x00}, TypeInt, "256"},
		{"uint32", []byte{0xce, 0x00, 0x01, 0x00, 0x00}, TypeInt, "65536"},
		{"uint64", []byte{0xcf, 0, 0, 0, 1, 0, 0, 0, 0}, TypeInt, "4294967296"},

		{"int8", []byte{0xd0, 0x80}, TypeInt, "-128"},
		{"int16", []byte{0xd1, 0x80, 0x00}, TypeInt, "-32768"},
		{"int32", []byte{0xd2, 0x80, 0, 0, 0}, TypeInt, "-2147483648"},
		{"int64", []byte{0xd3, 0x80, 0, 0, 0, 0, 0, 0, 0}, TypeInt, "-9223372036854775808"},

		{"float32", []byte{0xca, 0x3f, 0x80, 0x00, 0x00}, TypeFloat, "1.0"},
		{"float64", []byte{0xcb, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0}, TypeFloat, "1.0"},

		{"fixstr", []byte{0xa3, 'a', 'b', 'c'}, TypeString, "abc"},
		{"str8", []byte{0xd9, 0x03, 'a', 'b', 'c'}, TypeString, "abc"},
		{"str16", []byte{0xda, 0x00, 0x03, 'a', 'b', 'c'}, TypeString, "abc"},
		{"str32", []byte{0xdb, 0, 0, 0, 0x03, 'a', 'b', 'c'}, TypeString, "abc"},

		{"fixarray", []byte{0x92, 0x01, 0x02}, TypeArray, "[1,2]"},
		{"array16", []byte{0xdc, 0x00, 0x02, 0x01, 0x02}, TypeArray, "[1,2]"},
		{"array32", []byte{0xdd, 0, 0, 0, 0x02, 0x01, 0x02}, TypeArray, "[1,2]"},
		{"empty array", []byte{0x90}, TypeArray, "[]"},

		{"fixmap", []byte{0x81, 0xa1, 'a', 0x01}, TypeObject, `{"a":1}`},
		{"map16", []byte{0xde, 0x00, 0x01, 0xa1, 'a', 0x01}, TypeObject, `{"a":1}`},
		{"map32", []byte{0xdf, 0, 0, 0, 0x01, 0xa1, 'a', 0x01}, TypeObject, `{"a":1}`},
		{"empty map", []byte{0x80}, TypeObject, "{}"},

		// A key of any type is rendered with ToString.
		{"integer map key", []byte{0x81, 0x07, 0xa1, 'x'}, TypeObject, `{"7":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, err := ParseMsgPack(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if o.Type() != tc.typ {
				t.Errorf("type %s, want %s", o.Type(), tc.typ)
			}
			if got := o.ToString(); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// The bin family carries arbitrary bytes, so it is marked as binary.
func TestMsgPackDecodeBinary(t *testing.T) {
	for _, in := range [][]byte{
		{0xc4, 0x02, 0x01, 0x02},
		{0xc5, 0x00, 0x02, 0x01, 0x02},
		{0xc6, 0, 0, 0, 0x02, 0x01, 0x02},
	} {
		o, err := ParseMsgPack(in)
		if err != nil {
			t.Fatalf("%x: %v", in, err)
		}
		if o.Type() != TypeString || o.Str() != "\x01\x02" {
			t.Errorf("%x: got %s %q", in, o.Type(), o.Str())
		}
		if o.Flags()&FlagBinary == 0 {
			t.Errorf("%x: the binary flag is not set", in)
		}
	}
}

// UCL has no unsigned type, so a uint64 that does not fit an int64 keeps its
// magnitude as a float rather than wrapping into a negative number.
func TestMsgPackDecodeHugeUint(t *testing.T) {
	o, err := ParseMsgPack([]byte{0xcf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	if err != nil {
		t.Fatal(err)
	}
	if o.Type() != TypeFloat {
		t.Fatalf("type %s, want float", o.Type())
	}
	if o.Float() != math.MaxUint64 {
		t.Errorf("got %v, want %v", o.Float(), float64(math.MaxUint64))
	}
}

func TestMsgPackDecodeErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, "truncated"},
		{"truncated uint", []byte{0xcc}, "truncated"},
		{"truncated string", []byte{0xa3, 'a'}, "truncated"},
		{"truncated array", []byte{0x92, 0x01}, "truncated"},
		{"truncated map value", []byte{0x81, 0xa1, 'a'}, "truncated"},
		{"truncated length", []byte{0xda, 0x00}, "truncated"},
		{"unsupported ext", []byte{0xc7}, "unsupported msgpack type"},
		{"never used", []byte{0xc1}, "unsupported msgpack type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMsgPack(tc.in)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Only the first value is decoded; trailing bytes are ignored.
func TestMsgPackTrailingBytesIgnored(t *testing.T) {
	o, err := ParseMsgPack([]byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Fatal(err)
	}
	if o.Int() != 1 {
		t.Errorf("got %s", o.ToString())
	}
}

// The encoder picks the narrowest form that fits, so each width needs a value
// that forces it.
func TestMsgPackEncodeWidths(t *testing.T) {
	tests := []struct {
		name string
		obj  *Object
		tag  byte
	}{
		{"positive fixint", NewInt(127), 0x7f},
		{"negative fixint", NewInt(-32), 0xe0},
		{"int8", NewInt(-128), 0xd0},
		{"int16", NewInt(-32768), 0xd1},
		{"int32", NewInt(math.MinInt32), 0xd2},
		{"int64", NewInt(math.MinInt64), 0xd3},
		// 128 does not fit a signed 8 bit value, so the next width up is used.
		{"int16 for 128", NewInt(128), 0xd1},
		{"fixstr", NewString(strings.Repeat("x", 31)), 0xbf},
		{"str8", NewString(strings.Repeat("x", 32)), 0xd9},
		{"str16", NewString(strings.Repeat("x", 256)), 0xda},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.obj.Emit(MsgPack)
			if err != nil {
				t.Fatal(err)
			}
			if out[0] != tc.tag {
				t.Errorf("first byte 0x%02x, want 0x%02x", out[0], tc.tag)
			}
			back, err := ParseMsgPack(out)
			if err != nil {
				t.Fatal(err)
			}
			if back.ToString() != tc.obj.ToString() {
				t.Errorf("round trip gave %s, want %s", back.ToString(), tc.obj.ToString())
			}
		})
	}
}

// Time values and user data have no msgpack representation of their own.
func TestMsgPackEncodeSpecialTypes(t *testing.T) {
	obj := NewObject()
	obj.Set("time", NewSeconds(30))
	obj.Set("user", NewUserData("anything"))
	obj.Set("null", NewNull())

	out, err := obj.Emit(MsgPack)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseMsgPack(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Get("time").Float(); got != 30 {
		t.Errorf("time became %v", got)
	}
	if !back.Get("user").IsNull() || !back.Get("null").IsNull() {
		t.Errorf("got %s", back.ToString())
	}
}
