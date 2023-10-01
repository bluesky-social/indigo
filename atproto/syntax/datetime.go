package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

// Represents the a Datetime in string format, as would pass Lexicon syntax validation: the intersection of RFC-3339 and ISO-8601 syntax.
//
// Always use [ParseDatetime] instead of wrapping strings directly, especially when working with network input.
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
	return Datetime(raw), nil
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
