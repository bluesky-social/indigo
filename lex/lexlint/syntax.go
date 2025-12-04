package lexlint

import (
	"errors"
	"regexp"
)

var schemaNameRegex = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9]{0,255})?$`)

func CheckSchemaName(raw string) error {
	if raw == "" {
		return errors.New("empty name")
	}
	if len(raw) > 255 {
		return errors.New("name is too long (255 chars max)")
	}
	if !schemaNameRegex.MatchString(raw) {
		return errors.New("name doesn't match recommended syntax/characters")
	}
	return nil
}
