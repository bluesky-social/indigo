package util

import "encoding/json"

type typeExtractor struct {
	Type string `json:"type"`
}

func EnumTypeExtract(b []byte) (string, error) {
	var te typeExtractor
	if err := json.Unmarshal(b, &te); err != nil {
		return "", err
	}

	return te.Type, nil
}
