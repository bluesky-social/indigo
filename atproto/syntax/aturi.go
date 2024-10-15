package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var aturiRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)(\/(?P<collection>[a-zA-Z0-9-.]+)(\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512}))?)?$`)

// String type which represents a syntaxtually valid AT URI, as would pass Lexicon syntax validation for the 'at-uri' field (no query or fragment parts)
//
// Always use [ParseATURI] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/at-uri-scheme
type ATURI string

func ParseATURI(raw string) (ATURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("ATURI is too long (8192 chars max)")
	}
	parts := aturiRegex.FindStringSubmatch(raw)
	if parts == nil || len(parts) < 2 || parts[0] == "" {
		return "", errors.New("AT-URI syntax didn't validate via regex")
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

// Every valid ATURI has a valid AtIdentifier in the authority position.
//
// If this ATURI is malformed, returns empty
func (n ATURI) Authority() AtIdentifier {
	parts := strings.SplitN(string(n), "/", 4)
	if len(parts) < 3 {
		// something has gone wrong (would not validate)
		return AtIdentifier{}
	}
	atid, err := ParseAtIdentifier(parts[2])
	if err != nil {
		return AtIdentifier{}
	}
	return *atid
}

// Returns path segment, without leading slash, as would be used in an atproto repository key. Or empty string if there is no path.
func (n ATURI) Path() string {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 4 {
		// something has gone wrong (would not validate)
		return ""
	}
	if len(parts) == 4 {
		return parts[3]
	}
	return parts[3] + "/" + parts[4]
}

// Returns a valid NSID if there is one in the appropriate part of the path, otherwise empty.
func (n ATURI) Collection() NSID {
	parts := strings.SplitN(string(n), "/", 5)
	if len(parts) < 4 {
		// something has gone wrong (would not validate)
		return NSID("")
	}
	nsid, err := ParseNSID(parts[3])
	if err != nil {
		return NSID("")
	}
	return nsid
}

func (n ATURI) RecordKey() RecordKey {
	parts := strings.SplitN(string(n), "/", 6)
	if len(parts) < 5 {
		// something has gone wrong (would not validate)
		return RecordKey("")
	}
	rkey, err := ParseRecordKey(parts[4])
	if err != nil {
		return RecordKey("")
	}
	return rkey
}

func (n ATURI) Normalize() ATURI {
	auth := n.Authority()
	if auth.Inner == nil {
		// invalid AT-URI; return the current value (!)
		return n
	}
	coll := n.Collection()
	if coll == NSID("") {
		return ATURI("at://" + auth.Normalize().String())
	}
	rkey := n.RecordKey()
	if rkey == RecordKey("") {
		return ATURI("at://" + auth.Normalize().String() + "/" + coll.String())
	}
	return ATURI("at://" + auth.Normalize().String() + "/" + coll.Normalize().String() + "/" + rkey.String())
}

func (n ATURI) String() string {
	return string(n)
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
