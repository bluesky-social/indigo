package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var spaceuriRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)\/space\/(?P<stype>[a-zA-Z0-9.-]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})(\/(?P<repo>[a-zA-Z0-9._:%-]+)(\/(?P<collection>[a-zA-Z0-9-.]+)(\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512}))?)?)?$`)

// String type which represents a syntaxtually valid AT space URI referencing at least a space ("space ref"), and possibly an author repository, collection, and record.
//
// Use [ParseSpaceURI] instead of wrapping strings directly when working with external input.
//
// Syntax specification: https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#commit-digest
type SpaceURI string

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("Space URI is too long (8192 chars max)")
	}
	parts := spaceuriRegex.FindStringSubmatch(raw)
	//if parts == nil || len(parts) != 5 || parts[0] == "" {
	if parts == nil || parts[0] == "" {
		return "", errors.New("Space URI syntax didn't validate via regex")
	}
	// verify authority as a DID
	_, err := ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("Space URI authority section not a valid DID: %s", parts[1])
	}
	_, err = ParseNSID(parts[2])
	if err != nil {
		return "", fmt.Errorf("Space URI second path segment not an NSID: %s", parts[2])
	}
	_, err = ParseRecordKey(parts[3])
	if err != nil {
		return "", fmt.Errorf("Space URI third path segment not a space key (RecordKey): %s", parts[3])
	}

	if len(parts) >= 6 && parts[5] != "" {
		_, err := ParseDID(parts[5])
		if err != nil {
			return "", fmt.Errorf("Space URI fourth path segment not DID: %s", parts[5])
		}
	}
	if len(parts) >= 8 && parts[7] != "" {
		_, err := ParseNSID(parts[7])
		if err != nil {
			return "", fmt.Errorf("Space URI fifth path segment not an NSID: %s", parts[7])
		}
	}
	if len(parts) >= 10 && parts[9] != "" {
		_, err := ParseRecordKey(parts[9])
		if err != nil {
			return "", fmt.Errorf("Space URI sixth path segment not a RecordKey: %s", parts[9])
		}
	}
	return SpaceURI(raw), nil
}

// Every valid SpaceURI has a valid DID in the authority position.
//
// If this SpaceURI is malformed, returns empty
func (n SpaceURI) SpaceAuthority() DID {
	parts := strings.SplitN(string(n), "/", 4)
	if len(parts) < 3 {
		// something has gone wrong (would not validate)
		return ""
	}
	atid, err := ParseDID(parts[2])
	if err != nil {
		return ""
	}
	return atid
}

// Returns a valid NSID if there is one in the appropriate part of the path, otherwise empty.
func (n SpaceURI) SpaceType() NSID {
	parts := strings.SplitN(string(n), "/", 6)
	if len(parts) < 5 {
		// something has gone wrong (would not validate)
		return NSID("")
	}
	nsid, err := ParseNSID(parts[4])
	if err != nil {
		return NSID("")
	}
	return nsid
}

func (n SpaceURI) SpaceKey() RecordKey {
	parts := strings.SplitN(string(n), "/", 7)
	if len(parts) < 6 {
		// something has gone wrong (would not validate)
		return RecordKey("")
	}
	rkey, err := ParseRecordKey(parts[5])
	if err != nil {
		return RecordKey("")
	}
	return rkey
}

func (n SpaceURI) Repo() DID {
	parts := strings.SplitN(string(n), "/", 8)
	if len(parts) < 7 {
		// something has gone wrong (would not validate)
		return DID("")
	}
	did, err := ParseDID(parts[6])
	if err != nil {
		return DID("")
	}
	return did
}

// Returns a valid NSID if there is one in the appropriate part of the path, otherwise empty.
func (n SpaceURI) Collection() NSID {
	parts := strings.SplitN(string(n), "/", 9)
	if len(parts) < 8 {
		// something has gone wrong (would not validate)
		return NSID("")
	}
	nsid, err := ParseNSID(parts[7])
	if err != nil {
		return NSID("")
	}
	return nsid
}

func (n SpaceURI) RecordKey() RecordKey {
	parts := strings.SplitN(string(n), "/", 10)
	if len(parts) < 9 {
		// something has gone wrong (would not validate)
		return RecordKey("")
	}
	rkey, err := ParseRecordKey(parts[8])
	if err != nil {
		return RecordKey("")
	}
	return rkey
}

func (n SpaceURI) String() string {
	return string(n)
}

func (u SpaceURI) SpaceRef() SpaceRef {
	sauth := u.SpaceAuthority()
	stype := u.SpaceType()
	skey := u.SpaceKey()
	if sauth == "" || stype == "" || skey == "" {
		return ""
	}
	return SpaceRef(fmt.Sprintf("at://%s/space/%s/%s", sauth, stype, skey))
}

func (a SpaceURI) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *SpaceURI) UnmarshalText(text []byte) error {
	spaceuri, err := ParseSpaceURI(string(text))
	if err != nil {
		return err
	}
	*a = spaceuri
	return nil
}
