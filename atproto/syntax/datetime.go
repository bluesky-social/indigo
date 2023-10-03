package syntax

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// Prefered atproto Datetime string syntax, for use with [time.Format].
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

func ParseDatetime(raw string) (Datetime, error) {
	if len(raw) > 64 {
		return "", fmt.Errorf("Datetime too long (max 64 chars)")
	}
	var datetimeRegex = regexp.MustCompile(`^[0-9]{4}-[01][0-9]-[0-3][0-9]T[0-2][0-9]:[0-6][0-9]:[0-6][0-9](.[0-9]{1,20})?(Z|([+-][0-2][0-9]:[0-5][0-9]))$`)
	if !datetimeRegex.MatchString(raw) {
		return "", fmt.Errorf("Datetime syntax didn't validate via regex")
	}
	if strings.HasSuffix(raw, "-00:00") {
		return "", fmt.Errorf("Datetime can't use '-00:00' for UTC timezone, must use '+00:00', per ISO-8601")
	}
	// ensure that the datetime actually parses using golang time lib
	_, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", err
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

// Creates a new valid Datetime string matching the current time, in prefered syntax.
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
