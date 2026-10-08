package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var spaceuriRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)\/space\/(?P<stype>[a-zA-Z0-9.-]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})\/(?P<repo>[a-zA-Z0-9._:%-]+)\/(?P<collection>[a-zA-Z0-9.-]+)\/(?P<rkey>[a-zA-Z0-9_~.:-]{1,512})$`)

// SpaceURI is a complete record address in a space. Unlike ATURI, it requires
// both the authority and author to be DIDs and all record path components.
// Space references and repo/collection prefixes are not record addresses.
// Use ParseSpaceURI for external input.
//
// Syntax specification: https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#addressing
type SpaceURI string

func NewSpaceURI(ref SpaceRef, author DID, collection NSID, rkey RecordKey) SpaceURI {
	return SpaceURI(ref.String() + "/" + author.String() + "/" + collection.String() + "/" + rkey.String())
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("space URI is too long (8192 chars max)")
	}
	parts := spaceuriRegex.FindStringSubmatch(raw)
	if parts == nil {
		return "", errors.New("space URI must be a complete record address")
	}
	if _, err := ParseDID(parts[1]); err != nil {
		return "", fmt.Errorf("space URI authority: %w", err)
	}
	if _, err := ParseNSID(parts[2]); err != nil {
		return "", fmt.Errorf("space URI type: %w", err)
	}
	if _, err := ParseRecordKey(parts[3]); err != nil {
		return "", fmt.Errorf("space URI key: %w", err)
	}
	if _, err := ParseDID(parts[4]); err != nil {
		return "", fmt.Errorf("space URI author: %w", err)
	}
	if _, err := ParseNSID(parts[5]); err != nil {
		return "", fmt.Errorf("space URI collection: %w", err)
	}
	if _, err := ParseRecordKey(parts[6]); err != nil {
		return "", fmt.Errorf("space URI record key: %w", err)
	}
	return SpaceURI(raw), nil
}

func (n SpaceURI) SpaceAuthority() DID {
	return SpaceRef(n).SpaceAuthority()
}

func (n SpaceURI) SpaceType() NSID {
	return SpaceRef(n).SpaceType()
}

func (n SpaceURI) SpaceKey() RecordKey {
	return SpaceRef(n).SpaceKey()
}

func (n SpaceURI) Repo() DID {
	parts := strings.SplitN(string(n), "/", 8)
	if len(parts) < 7 {
		return ""
	}
	did, _ := ParseDID(parts[6])
	return did
}

func (n SpaceURI) Collection() NSID {
	parts := strings.SplitN(string(n), "/", 9)
	if len(parts) < 8 {
		return ""
	}
	nsid, _ := ParseNSID(parts[7])
	return nsid
}

func (n SpaceURI) RecordKey() RecordKey {
	parts := strings.SplitN(string(n), "/", 10)
	if len(parts) < 9 {
		return ""
	}
	key, _ := ParseRecordKey(parts[8])
	return key
}

func (n SpaceURI) String() string {
	return string(n)
}

func (u SpaceURI) SpaceRef() SpaceRef {
	authority, stype, skey := u.SpaceAuthority(), u.SpaceType(), u.SpaceKey()
	if authority == "" || stype == "" || skey == "" {
		return ""
	}
	return NewSpaceRef(authority, stype, skey)
}

func (a SpaceURI) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *SpaceURI) UnmarshalText(text []byte) error {
	uri, err := ParseSpaceURI(string(text))
	if err != nil {
		return err
	}
	*a = uri
	return nil
}
