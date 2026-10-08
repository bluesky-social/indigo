package lexicon

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// SchemaSpace declares a space type and its default write collections.
// Collections may belong to other namespace authorities but cannot be wildcards.
type SchemaSpace struct {
	Type        string            `json:"type"` // "space"
	Description *string           `json:"description,omitempty"`
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	NameLang    map[string]string `json:"name:lang,omitempty"`
	Collections []string          `json:"collections"`
}

func (s *SchemaSpace) CheckSchema() error {
	if s.Type != "space" {
		return fmt.Errorf("expected 'space' schema")
	}
	switch s.Key {
	case "":
		return fmt.Errorf("space key specifier is required")
	case "tid", "nsid", "any":
	default:
		literal, ok := strings.CutPrefix(s.Key, "literal:")
		if !ok {
			return fmt.Errorf("invalid space key specifier: %s", s.Key)
		}
		if _, err := syntax.ParseRecordKey(literal); err != nil {
			return fmt.Errorf("invalid literal space key: %w", err)
		}
	}
	if len(s.Name) < 1 || len(s.Name) > 64 {
		return fmt.Errorf("space name must be between 1 and 64 bytes long")
	}
	for lang, name := range s.NameLang {
		if _, err := syntax.ParseLanguage(lang); err != nil {
			return err
		}
		if len(name) < 1 || len(name) > 64 {
			return fmt.Errorf("localized space name must be between 1 and 64 bytes long")
		}
	}
	if s.Collections == nil {
		return fmt.Errorf("space collections is required")
	}
	for _, collection := range s.Collections {
		if _, err := syntax.ParseNSID(collection); err != nil {
			return fmt.Errorf("space collection: %w", err)
		}
	}
	return nil
}
