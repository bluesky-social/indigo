package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var spacerefRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)\/space\/(?P<stype>[a-zA-Z0-9.-]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`)

// String type which represents a syntaxtually valid AT space URI referencing a space, with space authority, space type, and space key.
//
// Use [ParseSpaceRef] instead of wrapping strings directly when working with external input.
//
// Syntax specification: https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#commit-digest
type SpaceRef string

func NewSpaceRef(authority DID, stype NSID, skey RecordKey) SpaceRef {
	return SpaceRef("at://" + authority.String() + "/space/" + stype.String() + "/" + skey.String())
}

func ParseSpaceRef(raw string) (SpaceRef, error) {
	if len(raw) > 8192 {
		return "", errors.New("Space ref is too long (8192 chars max)")
	}
	parts := spacerefRegex.FindStringSubmatch(raw)
	//if parts == nil || len(parts) != 5 || parts[0] == "" {
	if parts == nil || parts[0] == "" {
		return "", errors.New("Space ref syntax didn't validate via regex")
	}
	// verify authority as a DID
	_, err := ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("Space ref authority section not a valid DID: %s", parts[1])
	}
	_, err = ParseNSID(parts[2])
	if err != nil {
		return "", fmt.Errorf("Space ref second path segment not an NSID: %s", parts[2])
	}
	_, err = ParseRecordKey(parts[3])
	if err != nil {
		return "", fmt.Errorf("Space ref third path segment not a space key (RecordKey): %s", parts[3])
	}
	return SpaceRef(raw), nil
}

// Every valid SpaceRef has a valid DID in the authority position.
//
// If this SpaceRef is malformed, returns empty
func (n SpaceRef) SpaceAuthority() DID {
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

func (n SpaceRef) SpaceType() NSID {
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

func (n SpaceRef) SpaceKey() RecordKey {
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

func (n SpaceRef) String() string {
	return string(n)
}

func (a SpaceRef) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *SpaceRef) UnmarshalText(text []byte) error {
	spaceref, err := ParseSpaceRef(string(text))
	if err != nil {
		return err
	}
	*a = spaceref
	return nil
}
