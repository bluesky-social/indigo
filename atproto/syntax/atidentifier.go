package syntax

import (
	"errors"
	"strings"
)

type AtIdentifier struct {
	Inner interface{}
}

func ParseAtIdentifier(raw string) (*AtIdentifier, error) {
	if raw == "" {
		return nil, errors.New("expected AT account identifier, got empty string")
	}
	if strings.HasPrefix(raw, "did:") {
		did, err := ParseDID(raw)
		if err != nil {
			return nil, err
		}
		return &AtIdentifier{Inner: did}, nil
	}
	handle, err := ParseHandle(raw)
	if err != nil {
		return nil, err
	}
	return &AtIdentifier{Inner: handle}, nil
}

func (n AtIdentifier) IsHandle() bool {
	_, ok := n.Inner.(Handle)
	return ok
}

func (n AtIdentifier) AsHandle() (Handle, error) {
	handle, ok := n.Inner.(Handle)
	if ok {
		return handle, nil
	}
	return "", errors.New("AT Identifier is not a Handle")
}

func (n AtIdentifier) IsDID() bool {
	_, ok := n.Inner.(DID)
	return ok
}

func (n AtIdentifier) AsDID() (DID, error) {
	did, ok := n.Inner.(DID)
	if ok {
		return did, nil
	}
	return "", errors.New("AT Identifier is not a DID")
}

func (n AtIdentifier) Normalize() AtIdentifier {
	handle, ok := n.Inner.(Handle)
	if ok {
		return AtIdentifier{Inner: handle.Normalize()}
	}
	return n
}

func (n AtIdentifier) String() string {
	did, ok := n.Inner.(DID)
	if ok {
		return did.String()
	}
	handle, ok := n.Inner.(Handle)
	if ok {
		return handle.String()
	}
	return ""
}

func (a AtIdentifier) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *AtIdentifier) UnmarshalText(text []byte) error {
	atid, err := ParseAtIdentifier(string(text))
	if err != nil {
		return err
	}
	*a = *atid
	return nil
}
