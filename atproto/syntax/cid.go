package syntax

import (
	"errors"
	"regexp"
	"strings"
)

// Represents a CIDv1 in string format, as would pass Lexicon syntax validation.
//
// You usually want to use the github.com/ipfs/go-cid package and type when working with CIDs ("Links") in atproto. This specific type (syntax.CID) is an informal/incomplete helper specifically for doing fast string verification or pass-through without parsing, re-serialization, or normalization.
//
// Always use [ParseCID] instead of wrapping strings directly, especially when working with network input.
type CID string

var cidRegex = regexp.MustCompile(`^[a-zA-Z0-9+=]{8,256}$`)

func ParseCID(raw string) (CID, error) {
	if raw == "" {
		return "", errors.New("expected CID, got empty string")
	}
	if len(raw) > 256 {
		return "", errors.New("CID is too long (256 chars max)")
	}
	if len(raw) < 8 {
		return "", errors.New("CID is too short (8 chars min)")
	}

	if !cidRegex.MatchString(raw) {
		return "", errors.New("CID syntax didn't validate via regex")
	}
	if strings.HasPrefix(raw, "Qmb") {
		return "", errors.New("CIDv0 not allowed in this version of atproto")
	}
	return CID(raw), nil
}

func (c CID) String() string {
	return string(c)
}

func (c CID) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

func (c *CID) UnmarshalText(text []byte) error {
	cid, err := ParseCID(string(text))
	if err != nil {
		return err
	}
	*c = cid
	return nil
}
