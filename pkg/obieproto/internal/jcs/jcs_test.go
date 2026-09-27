package jcs

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"
)

// TestTransformRFCExample is the example of RFC 8785 section 3.2.2.
func TestTransformRFCExample(t *testing.T) {
	input := `{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`
	got, err := Transform([]byte(input))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if string(got) != want {
		t.Errorf("Transform() =\n%s\nwant\n%s", got, want)
	}
}

// TestTransformRFCSorting is the property sorting example of RFC 8785
// section 3.2.3: keys are ordered by UTF-16 code units, so the emoji
// (surrogate 0xd83d) sorts before U+FB33.
func TestTransformRFCSorting(t *testing.T) {
	input := `{
  "\u20ac": "Euro Sign",
  "\r": "Carriage Return",
  "\ufb33": "Hebrew Letter Dalet With Dagesh",
  "1": "One",
  "\ud83d\ude00": "Emoji: Grinning Face",
  "\u0080": "Control",
  "\u00f6": "Latin Small Letter O With Diaeresis"
}`
	want := "{" + strings.Join([]string{
		`"\r":"Carriage Return"`,
		`"1":"One"`,
		"\"\u0080\":\"Control\"",
		"\"\u00f6\":\"Latin Small Letter O With Diaeresis\"",
		"\"\u20ac\":\"Euro Sign\"",
		"\"\U0001F600\":\"Emoji: Grinning Face\"",
		"\"\ufb33\":\"Hebrew Letter Dalet With Dagesh\"",
	}, ",") + "}"
	got, err := Transform([]byte(input))
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if string(got) != want {
		t.Errorf("Transform() =\n%s\nwant\n%s", got, want)
	}
}

// TestFormatNumberRFCAppendixB covers the IEEE 754 samples of RFC 8785
// appendix B.
func TestFormatNumberRFCAppendixB(t *testing.T) {
	tests := []struct {
		bits string
		want string
	}{
		{"0000000000000000", "0"},
		{"8000000000000000", "0"},
		{"0000000000000001", "5e-324"},
		{"8000000000000001", "-5e-324"},
		{"7fefffffffffffff", "1.7976931348623157e+308"},
		{"ffefffffffffffff", "-1.7976931348623157e+308"},
		{"4340000000000000", "9007199254740992"},
		{"c340000000000000", "-9007199254740992"},
		{"4430000000000000", "295147905179352830000"},
		{"44b52d02c7e14af5", "9.999999999999997e+22"},
		{"44b52d02c7e14af6", "1e+23"},
		{"44b52d02c7e14af7", "1.0000000000000001e+23"},
		{"444b1ae4d6e2ef4e", "999999999999999700000"},
		{"444b1ae4d6e2ef4f", "999999999999999900000"},
		{"444b1ae4d6e2ef50", "1e+21"},
		{"3eb0c6f7a0b5ed8c", "9.999999999999997e-7"},
		{"3eb0c6f7a0b5ed8d", "0.000001"},
		{"41b3de4355555553", "333333333.3333332"},
		{"41b3de4355555554", "333333333.33333325"},
		{"41b3de4355555555", "333333333.3333333"},
		{"41b3de4355555556", "333333333.3333334"},
		{"41b3de4355555557", "333333333.33333343"},
		{"becbf647612f3696", "-0.0000033333333333333333"},
		{"43143ff3c1cb0959", "1424953923781206.2"},
	}
	for _, tt := range tests {
		t.Run(tt.bits, func(t *testing.T) {
			raw, err := hex.DecodeString(tt.bits)
			if err != nil {
				t.Fatal(err)
			}
			f := math.Float64frombits(binary.BigEndian.Uint64(raw))
			got, err := FormatNumber(f)
			if err != nil {
				t.Fatalf("FormatNumber(%v) error = %v", f, err)
			}
			if got != tt.want {
				t.Errorf("FormatNumber(%s) = %s, want %s", tt.bits, got, tt.want)
			}
		})
	}
}

func TestFormatNumberRejectsNonFinite(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := FormatNumber(f); err == nil {
			t.Errorf("FormatNumber(%v) error = nil, want error", f)
		}
	}
}

func TestTransformStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short escapes", `"\b\t\n\f\r"`, `"\b\t\n\f\r"`},
		{"other controls use lower-case hex", `"\u0000\u001F\u000B"`, `"\u0000\u001f\u000b"`},
		{"solidus and DEL are literal", `"\/\u007f"`, "\"/\u007f\""},
		{"html characters are literal", `"\u003c\u003e\u0026"`, `"<>&"`},
		{"line separators are literal", `"\u2028\u2029"`, "\"\u2028\u2029\""},
		{"surrogate pair becomes UTF-8", `"\uD83D\uDE00"`, "\"\U0001F600\""},
		{"escaped backslash before u", `"\\uD800"`, `"\\uD800"`},
		{"quote and backslash", `"\"\\"`, `"\"\\"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Transform([]byte(tt.input))
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Transform() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTransformStructure(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"whitespace removed", " { \"b\" : [ 1 , 2 ] , \"a\" : { } } ", `{"a":{},"b":[1,2]}`},
		{"nested objects sorted", `{"z":{"y":1,"x":2},"a":[]}`, `{"a":[],"z":{"x":2,"y":1}}`},
		{"scalars", `[true,false,null,"",-0,1.0,1e2]`, `[true,false,null,"",0,1,100]`},
		{"largest safe integer", `9007199254740991`, `9007199254740991`},
		{"large non-integer literal", `1e300`, `1e+300`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Transform([]byte(tt.input))
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Transform() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTransformRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid UTF-8", "\"\xff\""},
		{"lone high surrogate", `"\ud800"`},
		{"lone high surrogate at end of input", `"\ud800`},
		{"high surrogate followed by non-surrogate", `"\ud800\u0041"`},
		{"lone low surrogate", `"\udc00"`},
		{"lone surrogate in key", `{"\udfff":1}`},
		{"duplicate key", `{"a":1,"a":2}`},
		{"nested duplicate key", `{"a":{"b":1,"b":1}}`},
		{"integer above 2^53-1", `9007199254740992`},
		{"integer below -(2^53-1)", `-9007199254740993`},
		{"number overflows double", `1e400`},
		{"trailing data", `{} {}`},
		{"syntax error", `{"a":}`},
		{"empty input", ``},
		{"unterminated object", `{"a":1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Transform([]byte(tt.input)); err == nil {
				t.Errorf("Transform(%q) = %s, want error", tt.input, got)
			}
		})
	}
}

func TestEncodeRejects(t *testing.T) {
	for name, v := range map[string]any{
		"unsupported type":     42,
		"invalid UTF-8 string": "\xff",
		"invalid UTF-8 key":    map[string]any{"\xff": true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Encode(v); err == nil {
				t.Errorf("Encode(%#v) error = nil, want error", v)
			}
		})
	}
}

// FuzzTransform checks that Transform never panics and is idempotent: the
// canonical form of a canonical form is itself, unless it contains an integer
// beyond the I-JSON range (e.g. 1e20 canonicalizes to 100000000000000000000).
func FuzzTransform(f *testing.F) {
	for _, seed := range []string{`{"b":[1,2.5e-7,"\u00e9"],"a":null}`, `"\ud83d\ude00"`, `1e21`, `[]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Transform(data)
		if err != nil {
			return
		}
		again, err := Transform(out)
		if errors.Is(err, ErrUnsafeInteger) {
			return
		}
		if err != nil {
			t.Fatalf("Transform(canonical %q) error = %v", out, err)
		}
		if string(again) != string(out) {
			t.Fatalf("not idempotent: %q -> %q", out, again)
		}
	})
}
