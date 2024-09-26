package syntax

import (
	"errors"
	"regexp"
)

// Represents a Language specifier in string format, as would pass Lexicon syntax validation.
//
// Always use [ParseLanguage] instead of wrapping strings directly, especially when working with network input.
//
// The syntax is BCP-47. This is a partial/naive parsing implementation, designed for fast validation and exact-string passthrough with no normaliztion. For actually working with BCP-47 language specifiers in atproto code bases, we recommend the golang.org/x/text/language package.
type Language string

var langRegex = regexp.MustCompile(`^(i|[a-z]{2,3})(-[a-zA-Z0-9]+)*$`)

func ParseLanguage(raw string) (Language, error) {
	if raw == "" {
		return "", errors.New("expected language code, got empty string")
	}
	if len(raw) > 128 {
		return "", errors.New("Language is too long (128 chars max)")
	}
	if !langRegex.MatchString(raw) {
		return "", errors.New("Language syntax didn't validate via regex")
	}
	return Language(raw), nil
}

func (l Language) String() string {
	return string(l)
}

func (l Language) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

func (l *Language) UnmarshalText(text []byte) error {
	lang, err := ParseLanguage(string(text))
	if err != nil {
		return err
	}
	*l = lang
	return nil
}
