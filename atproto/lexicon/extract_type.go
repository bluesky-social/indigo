package lexicon

import (
	"encoding/json"
)

// Helper type for extracting schema type from schema JSON
type genericSchemaDef struct {
	Type string `json:"type"`
}

// Parses the top-level 'type' field from schema JSON data
func ExtractTypeJSON(b []byte) (string, error) {
	var gsd genericSchemaDef
	if err := json.Unmarshal(b, &gsd); err != nil {
		return "", err
	}

	return gsd.Type, nil
}
