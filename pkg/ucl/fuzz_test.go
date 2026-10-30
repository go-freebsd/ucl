package ucl

import "testing"

// FuzzParse checks that no input makes the parser panic, that every document
// can be emitted in every format, and that the formats which are themselves
// UCL documents parse back.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"a = 1",
		"a { b = [1, 2, 3] }",
		`{"a": "b"}`,
		"a = <<EOD\nbody\nEOD\n",
		"a = 'x'\nb = \"y\"\n",
		"# comment\n/* block */ a = 10s",
		".include \"x\"",
		"a = ${VAR}",
		"[1, 2]",
		"a=1;a=2;a=3",
		"0{0 0}",
		"000 <<A",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		obj, err := ParseString(src, WithoutMacros())
		if err != nil {
			return
		}
		for _, format := range []Format{JSON, JSONCompact, Config, YAML, MsgPack} {
			out, err := obj.Emit(format)
			if err != nil {
				t.Fatalf("emitting %s failed: %v", format, err)
			}
			switch format {
			case MsgPack:
				if _, err := ParseMsgPack(out); err != nil {
					t.Fatalf("msgpack round trip failed: %v", err)
				}
			case YAML:
				// Block style YAML is not a UCL document, so there is
				// nothing to parse back.
			default:
				if _, err := ParseString(string(out), WithoutMacros()); err != nil {
					t.Fatalf("reparsing %s output failed: %v\ninput: %q\noutput: %q",
						format, err, src, out)
				}
			}
		}
	})
}
