package ucl

import (
	"math"
	"strconv"
	"strings"
)

// Multipliers used by the numeric suffixes of libucl. A bare suffix uses
// powers of 1000, a suffix followed by "b" uses powers of 1024.
const (
	kilo = 1000
	mega = 1000 * 1000
	giga = 1000 * 1000 * 1000

	kibi = 1024
	mebi = 1024 * 1024
	gibi = 1024 * 1024 * 1024
)

// Seconds per time suffix.
const (
	secMinute = 60
	secHour   = 60 * 60
	secDay    = 24 * 60 * 60
	secWeek   = 7 * 24 * 60 * 60
	secYear   = 365 * 24 * 60 * 60
)

// suffixKind distinguishes the three families of numeric suffixes, which
// libucl treats differently.
type suffixKind uint8

const (
	// sufNone is a plain number.
	sufNone suffixKind = iota
	// sufSize is k, m or g, a decimal multiplier that keeps the value type.
	sufSize
	// sufBytes is kb, mb or gb, a binary multiplier that always yields an
	// integer.
	sufBytes
	// sufTime is any of the time suffixes and always yields a time value.
	sufTime
)

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// parseBool recognises the boolean spellings accepted by libucl: true, false,
// yes, no, on and off, all case insensitive.
func parseBool(s string) (val bool, ok bool) {
	switch len(s) {
	case 2:
		switch {
		case strings.EqualFold(s, "no"):
			return false, true
		case strings.EqualFold(s, "on"):
			return true, true
		}
	case 3:
		switch {
		case strings.EqualFold(s, "yes"):
			return true, true
		case strings.EqualFold(s, "off"):
			return false, true
		}
	case 4:
		if strings.EqualFold(s, "true") {
			return true, true
		}
	case 5:
		if strings.EqualFold(s, "false") {
			return false, true
		}
	}
	return false, false
}

// parseNumber parses a UCL number including its optional suffix. It returns
// nil unless the whole string is a number, so that tokens such as "1.2.3",
// "10 20" or "+3" stay strings, exactly like libucl which only attempts a
// number when the token starts with a digit or a minus sign.
func parseNumber(s string, allowTime bool) *Object {
	if s == "" || !(isDigit(s[0]) || s[0] == '-') {
		return nil
	}
	i := 0
	neg := s[0] == '-'
	if neg {
		i++
	}
	if i >= len(s) {
		return nil
	}
	switch s[i:] {
	case "inf":
		return NewFloat(math.Inf(sign(neg)))
	case "nan":
		return NewFloat(math.NaN())
	}

	// Hexadecimal integers carry no suffix.
	if s[i] == '0' && i+1 < len(s) && (s[i+1] == 'x' || s[i+1] == 'X') {
		digits := s[i+2:]
		if digits == "" {
			return nil
		}
		for j := 0; j < len(digits); j++ {
			if !isHexDigit(digits[j]) {
				return nil
			}
		}
		v, err := strconv.ParseInt(digits, 16, 64)
		if err != nil {
			return nil
		}
		if neg {
			v = -v
		}
		return NewInt(v)
	}

	start := i
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	intDigits := i - start
	isFloat := false
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j > i+1 {
			isFloat = true
			i = j
		}
	}
	if intDigits == 0 && !isFloat {
		return nil
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && isDigit(s[k]) {
			k++
		}
		if k > j {
			isFloat = true
			i = k
		}
	}

	lit := s[start:i]
	mult, kind, ok := numberSuffix(s[i:], allowTime)
	if !ok {
		return nil
	}

	switch kind {
	case sufTime:
		dv, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return nil
		}
		return NewSeconds(copySign(dv*mult, neg))
	case sufBytes:
		// libucl truncates a fractional value before applying a binary
		// multiplier, so 1.5kb is 1024 and not 1536.
		base, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return nil
		}
		return NewInt(int64(copySign(float64(int64(base))*mult, neg)))
	}

	if !isFloat {
		if iv, err := strconv.ParseInt(lit, 10, 64); err == nil {
			scaled := iv * int64(mult)
			if mult == 1 || iv == 0 || scaled/int64(mult) == iv {
				if neg {
					scaled = -scaled
				}
				return NewInt(scaled)
			}
		}
	}
	dv, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return nil
	}
	return NewFloat(copySign(dv*mult, neg))
}

func sign(neg bool) int {
	if neg {
		return -1
	}
	return 1
}

func copySign(v float64, neg bool) float64 {
	if neg {
		return -v
	}
	return v
}

// numberSuffix decodes a numeric suffix and reports whether it is valid.
func numberSuffix(s string, allowTime bool) (mult float64, kind suffixKind, ok bool) {
	if s == "" {
		return 1, sufNone, true
	}
	switch strings.ToLower(s) {
	case "k":
		return kilo, sufSize, true
	case "m":
		return mega, sufSize, true
	case "g":
		return giga, sufSize, true
	case "kb":
		return kibi, sufBytes, true
	case "mb":
		return mebi, sufBytes, true
	case "gb":
		return gibi, sufBytes, true
	}
	if !allowTime {
		return 0, sufNone, false
	}
	switch strings.ToLower(s) {
	case "s":
		return 1, sufTime, true
	case "ms":
		return 0.001, sufTime, true
	case "ks":
		return kilo, sufTime, true
	case "gs":
		return giga, sufTime, true
	case "min":
		return secMinute, sufTime, true
	case "h":
		return secHour, sufTime, true
	case "d":
		return secDay, sufTime, true
	case "w":
		return secWeek, sufTime, true
	case "y":
		return secYear, sufTime, true
	}
	return 0, sufNone, false
}
