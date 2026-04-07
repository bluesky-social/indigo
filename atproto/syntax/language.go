package syntax

import (
	"errors"
)

// Represents a Language specifier in string format, as would pass Lexicon syntax validation.
//
// Always use [ParseLanguage] instead of wrapping strings directly, especially when working with network input.
//
// The syntax is BCP-47. This is a partial/naive parsing implementation, designed for fast validation and exact-string passthrough with no normaliztion. For actually working with BCP-47 language specifiers in atproto code bases, we recommend the golang.org/x/text/language package.
type Language string

func ParseLanguage(raw string) (Language, error) {
	if raw == "" {
		return "", errors.New("expected language code, got empty string")
	}
	if len(raw) > 128 {
		return "", errors.New("Language is too long (128 chars max)")
	}

	// Primary subtag: "i" or 2-3 lowercase letters.
	i := 0
	for i < len(raw) && raw[i] != '-' {
		if !isLowerAlpha(raw[i]) {
			return "", errors.New("Language syntax didn't validate")
		}
		i++
	}

	if i == 0 {
		return "", errors.New("Language syntax didn't validate")
	}
	if i == 1 && raw[0] != 'i' {
		return "", errors.New("Language syntax didn't validate")
	}
	if i > 3 {
		return "", errors.New("Language syntax didn't validate")
	}

	// Subsequent subtags: hyphen-separated alphanumeric.
	for i < len(raw) {
		if raw[i] != '-' {
			return "", errors.New("Language syntax didn't validate")
		}
		i++ // skip hyphen
		start := i
		for i < len(raw) && raw[i] != '-' {
			if !isAlphanumeric(raw[i]) {
				return "", errors.New("Language syntax didn't validate")
			}
			i++
		}
		if i == start {
			return "", errors.New("Language syntax didn't validate")
		}
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
