package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

var aturiRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)(\/(?P<collection>[a-zA-Z0-9-.]+)(\/(?P<rkey>[a-zA-Z0-9_~.-]{1,512}))?)?$`)

// String type which represents a syntaxtually valid AT URI, as would pass Lexicon syntax validation for the 'at-uri' field (no query or fragment parts)
//
// Always use [ParseATURI] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/at-uri-scheme
type ATURI string

func ParseATURI(raw string) (ATURI, error) {
	if len(raw) > 8192 {
		return "", fmt.Errorf("ATURI is too long (8192 chars max)")
	}
	parts := aturiRegex.FindStringSubmatch(raw)
	if parts == nil || len(parts) < 2 || parts[0] == "" {
		return "", fmt.Errorf("AT-URI syntax didn't validate via regex")
	}
	// verify authority as either a DID or NSID
	_, err := ParseAtIdentifier(parts[1])
	if err != nil {
		return "", fmt.Errorf("AT-URI authority section neither a DID nor Handle: %s", parts[1])
	}
	if len(parts) >= 4 && parts[3] != "" {
		_, err := ParseNSID(parts[3])
		if err != nil {
			return "", fmt.Errorf("AT-URI first path segment not an NSID: %s", parts[3])
		}
	}
	if len(parts) >= 6 && parts[5] != "" {
		_, err := ParseRecordKey(parts[5])
		if err != nil {
			return "", fmt.Errorf("AT-URI second path segment not a RecordKey: %s", parts[5])
		}
	}
	return ATURI(raw), nil
}

func (n ATURI) Authority() (*AtIdentifier, error) {
	parts := strings.SplitN(string(n), "/", 4)
	if len(parts) < 3 {
		// something has gone wrong (would not validate)
		return nil, fmt.Errorf("AT-URI has no authority segment (invalid)")
	}
	return ParseAtIdentifier(parts[2])
}

// Returns path segment, without leading slash, as would be used in an atproto repository key
func (n ATURI) Path() string {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 3 {
		// something has gone wrong (would not validate)
		return ""
	}
	if len(parts) == 3 {
		return parts[2]
	}
	return parts[2] + "/" + parts[3]
}

func (n ATURI) Collection() (NSID, error) {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 4 {
		// something has gone wrong (would not validate)
		return NSID(""), fmt.Errorf("AT-URI has no collection segment")
	}
	// re-parsing is safest
	return ParseNSID(parts[3])
}

func (n ATURI) RecordKey() (RecordKey, error) {
	parts := strings.SplitN(string(n), "/", 6)
	if len(parts) < 5 {
		// something has gone wrong (would not validate)
		return RecordKey(""), fmt.Errorf("AT-URI has no record key segment")
	}
	// re-parsing is safest
	return ParseRecordKey(parts[4])
}

func (n ATURI) Normalize() ATURI {
	auth, err := n.Authority()
	if err != nil {
		// invalid AT-URI
		return n
	}
	coll, err := n.Collection()
	if err != nil {
		return ATURI("at://" + auth.Normalize().String())
	}
	rkey, err := n.RecordKey()
	if err != nil {
		return ATURI("at://" + auth.Normalize().String() + "/" + coll.String())
	}
	return ATURI("at://" + auth.Normalize().String() + "/" + coll.Normalize().String() + "/" + rkey.String())
}

func (a ATURI) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *ATURI) UnmarshalText(text []byte) error {
	aturi, err := ParseATURI(string(text))
	if err != nil {
		return err
	}
	*a = aturi
	return nil
}
