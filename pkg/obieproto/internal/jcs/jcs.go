// Package jcs implements the JSON Canonicalization Scheme of RFC 8785.
//
// Input must be I-JSON (RFC 7493): valid UTF-8 without lone surrogates, no
// duplicate object keys, finite numbers, and integers within the range an
// IEEE 754 double represents exactly. Anything else is an error rather than
// being silently repaired, so that two parties can never disagree about the
// canonical form of the same input.
package jcs

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// maxSafeInteger is 2^53 - 1, the largest integer every double represents
// exactly (I-JSON, RFC 7493 section 2.2).
const maxSafeInteger = 1<<53 - 1

// ErrUnsafeInteger reports an integer literal outside ±(2^53-1). Such a
// literal may not survive the conversion to a double that canonicalization
// implies, so accepting it would let two different inputs share one
// canonical form. Note that the canonical form of a large non-integer
// literal below 1e21 can itself be such an integer.
var ErrUnsafeInteger = errors.New("jcs: integer outside the I-JSON range ±(2^53-1)")

// Transform returns the canonical form of the JSON text data.
func Transform(data []byte) ([]byte, error) {
	v, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return Encode(v)
}

// Parse decodes a single JSON value into the generic form [Encode] accepts:
// map[string]any, []any, string, json.Number, bool and nil.
func Parse(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("jcs: input is not valid UTF-8")
	}
	if err := checkSurrogates(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("jcs: trailing data after the JSON value")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("jcs: %w", err)
	}
	switch tok {
	case json.Delim('{'):
		return parseObject(dec)
	case json.Delim('['):
		return parseArray(dec)
	}
	return tok, nil
}

func parseObject(dec *json.Decoder) (any, error) {
	obj := map[string]any{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("jcs: %w", err)
		}
		key, _ := tok.(string)
		if _, dup := obj[key]; dup {
			return nil, fmt.Errorf("jcs: duplicate key %q", key)
		}
		if obj[key], err = parseValue(dec); err != nil {
			return nil, err
		}
	}
	_, err := dec.Token() // '}'
	return obj, err
}

func parseArray(dec *json.Decoder) (any, error) {
	arr := []any{}
	for dec.More() {
		v, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
	}
	_, err := dec.Token() // ']'
	return arr, err
}

// checkSurrogates rejects \u escapes that encode a lone UTF-16 surrogate,
// which encoding/json would otherwise silently replace with U+FFFD. Outside
// strings a backslash is a syntax error that the decoder reports, so the scan
// does not need to track string boundaries.
func checkSurrogates(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		r, ok := escapedRune(data, i)
		i++ // skip the escaped character
		if !ok || !utf16.IsSurrogate(r) {
			continue
		}
		low, ok := escapedRune(data, i+5)
		if r >= 0xdc00 || !ok || utf16.DecodeRune(r, low) == utf8.RuneError {
			return errors.New("jcs: lone UTF-16 surrogate in string")
		}
		i += 10 // skip both escapes
	}
	return nil
}

// escapedRune decodes the \uXXXX escape starting at data[i], if any.
func escapedRune(data []byte, i int) (rune, bool) {
	if i+6 > len(data) || data[i] != '\\' || data[i+1] != 'u' {
		return 0, false
	}
	n, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
	return rune(n), err == nil
}

// Encode returns the canonical JSON text of v, which must consist of the
// types [Parse] produces.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any) error {
	switch v := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(v))
	case string:
		return writeString(buf, v)
	case json.Number:
		return writeNumber(buf, v)
	case []any:
		return writeArray(buf, v)
	case map[string]any:
		return writeObject(buf, v)
	default:
		return fmt.Errorf("jcs: unsupported type %T", v)
	}
	return nil
}

func writeArray(buf *bytes.Buffer, arr []any) error {
	buf.WriteByte('[')
	for i, elem := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encode(buf, elem); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// writeObject writes the members sorted by the UTF-16 code units of their
// keys (RFC 8785 section 3.2.3).
func writeObject(buf *bytes.Buffer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compareUTF16)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeString(buf, k); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := encode(buf, obj[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// compareUTF16 orders strings by their UTF-16 code units without converting
// them. Code point order, which UTF-8 byte order follows, differs from UTF-16
// order only where a supplementary character (encoded with a leading
// surrogate 0xd800-0xdbff) meets a character in 0xe000-0xffff.
func compareUTF16(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			if c := cmp.Compare(firstUnit(ra), firstUnit(rb)); c != 0 {
				return c
			}
			return cmp.Compare(ra, rb) // same leading surrogate: order of the trailing one
		}
		a, b = a[na:], b[nb:]
	}
	return cmp.Compare(len(a), len(b))
}

// firstUnit returns the first UTF-16 code unit of r.
func firstUnit(r rune) rune {
	if r < 0x10000 {
		return r
	}
	high, _ := utf16.EncodeRune(r)
	return high
}

// writeString escapes only what RFC 8785 section 3.2.2.2 requires: the
// quotation mark, the backslash and the control characters below U+0020.
func writeString(buf *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return errors.New("jcs: string is not valid UTF-8")
	}
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			buf.WriteByte('\\')
			buf.WriteByte(c)
		case c == '\b':
			buf.WriteString(`\b`)
		case c == '\t':
			buf.WriteString(`\t`)
		case c == '\n':
			buf.WriteString(`\n`)
		case c == '\f':
			buf.WriteString(`\f`)
		case c == '\r':
			buf.WriteString(`\r`)
		case c < 0x20:
			fmt.Fprintf(buf, `\u%04x`, c)
		default:
			buf.WriteByte(c)
		}
	}
	buf.WriteByte('"')
	return nil
}

func writeNumber(buf *bytes.Buffer, n json.Number) error {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return fmt.Errorf("jcs: number %s is not a finite double", n)
	}
	if !strings.ContainsAny(string(n), ".eE") && math.Abs(f) > maxSafeInteger {
		return fmt.Errorf("%w: %s", ErrUnsafeInteger, n)
	}
	s, err := FormatNumber(f)
	if err != nil {
		return err
	}
	buf.WriteString(s)
	return nil
}

// FormatNumber serializes f like ECMAScript's Number.prototype.toString
// (ECMA-262 section 7.1.12.1), as RFC 8785 section 3.2.2.3 requires.
func FormatNumber(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("jcs: %v is not a finite number", f)
	}
	if f == 0 {
		return "0", nil // also -0
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// The shortest round-tripping digits d1...dk and exponent: f = 0.d1...dk × 10^n.
	mantissa, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	k, n := len(digits), e+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k), nil
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:], nil
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits, nil
	}
	s := digits[:1]
	if k > 1 {
		s += "." + digits[1:]
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	return sign + s + "e" + expSign + strconv.Itoa(abs(n-1)), nil
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}
