package syntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var spacerefRegex = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)\/space\/(?P<stype>[a-zA-Z0-9.-]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`)

// SpaceRef identifies exactly one space by its authority DID, type NSID, and key.
// Use ParseSpaceRef for external input. The key has RecordKey syntax.
//
// Syntax specification: https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#addressing
type SpaceRef string

func NewSpaceRef(authority DID, stype NSID, skey RecordKey) SpaceRef {
	return SpaceRef("at://" + authority.String() + "/space/" + stype.String() + "/" + skey.String())
}

func ParseSpaceRef(raw string) (SpaceRef, error) {
	if len(raw) > 8192 {
		return "", errors.New("space ref is too long (8192 chars max)")
	}
	parts := spacerefRegex.FindStringSubmatch(raw)
	if parts == nil {
		return "", errors.New("space ref syntax didn't validate via regex")
	}
	if _, err := ParseDID(parts[1]); err != nil {
		return "", fmt.Errorf("space ref authority: %w", err)
	}
	if _, err := ParseNSID(parts[2]); err != nil {
		return "", fmt.Errorf("space ref type: %w", err)
	}
	if _, err := ParseRecordKey(parts[3]); err != nil {
		return "", fmt.Errorf("space ref key: %w", err)
	}
	return SpaceRef(raw), nil
}

// SpaceAuthority returns the authority DID, or empty if that component is invalid.
func (n SpaceRef) SpaceAuthority() DID {
	parts := strings.SplitN(string(n), "/", 4)
	if len(parts) < 3 {
		return ""
	}
	did, _ := ParseDID(parts[2])
	return did
}

func (n SpaceRef) SpaceType() NSID {
	parts := strings.SplitN(string(n), "/", 6)
	if len(parts) < 5 {
		return ""
	}
	nsid, _ := ParseNSID(parts[4])
	return nsid
}

func (n SpaceRef) SpaceKey() RecordKey {
	parts := strings.SplitN(string(n), "/", 7)
	if len(parts) < 6 {
		return ""
	}
	key, _ := ParseRecordKey(parts[5])
	return key
}

func (n SpaceRef) String() string {
	return string(n)
}

func (a SpaceRef) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *SpaceRef) UnmarshalText(text []byte) error {
	ref, err := ParseSpaceRef(string(text))
	if err != nil {
		return err
	}
	*a = ref
	return nil
}
