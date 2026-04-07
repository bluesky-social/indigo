package syntax

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Preferred atproto Datetime string syntax, for use with [time.Format].
	//
	// Note that *parsing* syntax is more flexible.
	AtprotoDatetimeLayout = "2006-01-02T15:04:05.999Z"
)

// Represents the a Datetime in string format, as would pass Lexicon syntax validation: the intersection of RFC-3339 and ISO-8601 syntax.
//
// Always use [ParseDatetime] instead of wrapping strings directly, especially when working with network input.
//
// Syntax is specified at: https://atproto.com/specs/lexicon#datetime
type Datetime string

var astroZero = time.Date(0000, 1, 1, 0, 0, 0, 0, time.UTC)

// validateDatetimeSyntax checks the structural format without semantic validation.
// Numeric positions use isDigit() rather than range-checking (e.g. [01] for month
// tens digit). Semantic validation of impossible dates (month 13, hour 25, etc.)
// is delegated to time.Parse(time.RFC3339Nano) in ParseDatetime.
func validateDatetimeSyntax(raw string) error {
	n := len(raw)
	if n < 20 { // Minimum: "0000-01-01T00:00:00Z"
		return errors.New("Datetime syntax didn't validate")
	}

	// YYYY-MM-DD
	if !isDigit(raw[0]) || !isDigit(raw[1]) || !isDigit(raw[2]) || !isDigit(raw[3]) {
		return errors.New("Datetime syntax didn't validate")
	}
	if raw[4] != '-' {
		return errors.New("Datetime syntax didn't validate")
	}
	if !isDigit(raw[5]) || !isDigit(raw[6]) {
		return errors.New("Datetime syntax didn't validate")
	}
	if raw[7] != '-' {
		return errors.New("Datetime syntax didn't validate")
	}
	if !isDigit(raw[8]) || !isDigit(raw[9]) {
		return errors.New("Datetime syntax didn't validate")
	}

	// T
	if raw[10] != 'T' {
		return errors.New("Datetime syntax didn't validate")
	}

	// hh:mm:ss
	if !isDigit(raw[11]) || !isDigit(raw[12]) {
		return errors.New("Datetime syntax didn't validate")
	}
	if raw[13] != ':' {
		return errors.New("Datetime syntax didn't validate")
	}
	if !isDigit(raw[14]) || !isDigit(raw[15]) {
		return errors.New("Datetime syntax didn't validate")
	}
	if raw[16] != ':' {
		return errors.New("Datetime syntax didn't validate")
	}
	if !isDigit(raw[17]) || !isDigit(raw[18]) {
		return errors.New("Datetime syntax didn't validate")
	}

	i := 19

	// Optional fractional seconds.
	if i < n && raw[i] == '.' {
		i++
		fracStart := i
		for i < n && isDigit(raw[i]) {
			i++
		}
		fracLen := i - fracStart
		if fracLen == 0 || fracLen > 20 {
			return errors.New("Datetime syntax didn't validate")
		}
	}

	// Timezone: Z or [+-]hh:mm
	if i >= n {
		return errors.New("Datetime syntax didn't validate")
	}
	switch raw[i] {
	case 'Z':
		i++
	case '+', '-':
		i++
		if i+5 > n {
			return errors.New("Datetime syntax didn't validate")
		}
		if !isDigit(raw[i]) || !isDigit(raw[i+1]) {
			return errors.New("Datetime syntax didn't validate")
		}
		if raw[i+2] != ':' {
			return errors.New("Datetime syntax didn't validate")
		}
		if !isDigit(raw[i+3]) || !isDigit(raw[i+4]) {
			return errors.New("Datetime syntax didn't validate")
		}
		i += 5
	default:
		return errors.New("Datetime syntax didn't validate")
	}

	if i != n {
		return errors.New("Datetime syntax didn't validate")
	}

	return nil
}

func ParseDatetime(raw string) (Datetime, error) {
	if raw == "" {
		return "", errors.New("expected datetime, got empty string")
	}
	if len(raw) > 64 {
		return "", errors.New("Datetime too long (max 64 chars)")
	}

	if err := validateDatetimeSyntax(raw); err != nil {
		return "", err
	}

	if strings.HasSuffix(raw, "-00:00") {
		return "", errors.New("Datetime can't use '-00:00' for UTC timezone, must use '+00:00', per ISO-8601")
	}
	// ensure that the datetime actually parses using golang time lib
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", err
	}
	// times before astronomical zero are disallowed
	if t.Before(astroZero) {
		return "", errors.New("Datetime can not be before year zero")
	}
	return Datetime(raw), nil
}

// Validates and converts a string to a golang [time.Time] in a single step.
func ParseDatetimeTime(raw string) (time.Time, error) {
	d, err := ParseDatetime(raw)
	if err != nil {
		var zero time.Time
		return zero, err
	}
	return d.Time(), nil
}

// Similar to ParseDatetime, but more flexible about some parsing.
//
// Note that this may mutate the internal string, so a round-trip will fail. This is intended for working with legacy/broken records, not to be used in an ongoing way.
func hasTimezone(s string) bool {
	if len(s) == 0 {
		return false
	}
	last := s[len(s)-1]
	if last == 'Z' || (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
		return true
	}
	// Check for +hh:mm or -hh:mm at end.
	if len(s) >= 6 {
		c := s[len(s)-6]
		if c == '+' || c == '-' {
			return true
		}
	}
	// Check for +hhmm or -hhmm (without colon).
	if len(s) >= 5 {
		c := s[len(s)-5]
		if c == '+' || c == '-' {
			return true
		}
	}
	return false
}

func ParseDatetimeLenient(raw string) (Datetime, error) {
	// fast path: it is a valid overall datetime
	valid, err := ParseDatetime(raw)
	if nil == err {
		return valid, nil
	}

	if strings.HasSuffix(raw, "-00:00") {
		return ParseDatetime(strings.Replace(raw, "-00:00", "+00:00", 1))
	}
	if strings.HasSuffix(raw, "-0000") {
		return ParseDatetime(strings.Replace(raw, "-0000", "+00:00", 1))
	}
	if strings.HasSuffix(raw, "+0000") {
		return ParseDatetime(strings.Replace(raw, "+0000", "+00:00", 1))
	}

	// try adding timezone if it is missing
	if !hasTimezone(raw) {
		withTZ, err := ParseDatetime(raw + "Z")
		if nil == err {
			return withTZ, nil
		}
	}

	return "", fmt.Errorf("Datetime could not be parsed, even leniently: %v", err)
}

// Parses the Datetime string in to a golang [time.Time].
//
// This method assumes that [ParseDatetime] was used to create the Datetime, which already verified parsing, and thus that [time.Parse] will always succeed. In the event of an error, zero/nil will be returned.
func (d Datetime) Time() time.Time {
	var zero time.Time
	ret, err := time.Parse(time.RFC3339Nano, d.String())
	if err != nil {
		return zero
	}
	return ret
}

// Creates a new valid Datetime string matching the current time, in preferred syntax.
func DatetimeNow() Datetime {
	t := time.Now().UTC()
	return Datetime(t.Format(AtprotoDatetimeLayout))
}

func (d Datetime) String() string {
	return string(d)
}

func (d Datetime) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *Datetime) UnmarshalText(text []byte) error {
	datetime, err := ParseDatetime(string(text))
	if err != nil {
		return err
	}
	*d = datetime
	return nil
}
