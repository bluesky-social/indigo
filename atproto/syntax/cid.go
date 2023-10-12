package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

// Represents a CIDv1 in string format, as would pass Lexicon syntax validation.
//
// You usually want to use the github.com/ipfs/go-cid package and type when working with CIDs ("Links") in atproto. This specific type (syntax.CID) is an informal/incomplete helper specifically for doing fast string verification or pass-through without parsing, re-serialization, or normalization.
//
// Always use [ParseCID] instead of wrapping strings directly, especially when working with network input.
type CID string

func ParseCID(raw string) (CID, error) {
	if len(raw) > 256 {
		return "", fmt.Errorf("CID is too long (256 chars max)")
	}
	if len(raw) < 8 {
		return "", fmt.Errorf("CID is too short (8 chars min)")
	}
	var cidRegex = regexp.MustCompile(`^[a-zA-Z0-9+=]{8,256}$`)
	if !cidRegex.MatchString(raw) {
		return "", fmt.Errorf("CID syntax didn't validate via regex")
	}
	if strings.HasPrefix(raw, "Qmb") {
		return "", fmt.Errorf("CIDv0 not allowed in this version of atproto")
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
