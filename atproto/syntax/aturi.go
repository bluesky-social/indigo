package syntax

import (
	"errors"
	"fmt"
	"strings"
)

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
	if len(raw) < 5 || raw[:5] != "at://" {
		return "", errors.New("AT-URI syntax didn't vaidate")
	}

	// Reject query and fragment.
	for i := 5; i < len(raw); i++ {
		if raw[i] == '?' || raw[i] == '#' {
			return "", errors.New("AT-URI syntax didn't vaidate")
		}
	}

	rest := raw[5:]
	if len(rest) == 0 {
		return "", errors.New("AT-URI syntax didn't vaidate")
	}

	// Find the first slash to separate authority from path.
	slash1 := strings.IndexByte(rest, '/')
	var authority string
	if slash1 < 0 {
		authority = rest
	} else {
		authority = rest[:slash1]
	}

	// verify authority as either a DID or Handle
	_, err := ParseAtIdentifier(authority)
	if err != nil {
		return "", fmt.Errorf("AT-URI authority section neither a DID nor Handle: %s", authority)
	}

	// No path — just authority.
	if slash1 < 0 {
		return ATURI(raw), nil
	}

	afterAuth := rest[slash1+1:]
	if len(afterAuth) == 0 {
		return "", errors.New("AT-URI syntax didn't vaidate")
	}

	// Find second slash to separate collection from rkey.
	slash2 := strings.IndexByte(afterAuth, '/')
	var collection string
	if slash2 < 0 {
		collection = afterAuth
	} else {
		collection = afterAuth[:slash2]
	}

	_, err = ParseNSID(collection)
	if err != nil {
		return "", fmt.Errorf("AT-URI first path segment not an NSID: %s", collection)
	}

	// No record key.
	if slash2 < 0 {
		return ATURI(raw), nil
	}

	afterColl := afterAuth[slash2+1:]
	if len(afterColl) == 0 {
		return "", errors.New("AT-URI syntax didn't vaidate")
	}

	// Reject additional path segments.
	if strings.IndexByte(afterColl, '/') >= 0 {
		return "", errors.New("AT-URI syntax didn't vaidate")
	}

	_, err = ParseRecordKey(afterColl)
	if err != nil {
		return "", fmt.Errorf("AT-URI second path segment not a RecordKey: %s", afterColl)
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
		return ""
	}
	atid, err := ParseAtIdentifier(parts[2])
	if err != nil {
		return ""
	}
	return atid
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
	if auth == "" {
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
