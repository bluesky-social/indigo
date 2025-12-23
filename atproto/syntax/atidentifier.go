package syntax

import (
	"errors"
	"strings"
)

type AtIdentifier string

func ParseAtIdentifier(raw string) (AtIdentifier, error) {
	if raw == "" {
		return "", errors.New("expected AT account identifier, got empty string")
	}
	if strings.HasPrefix(raw, "did:") {
		did, err := ParseDID(raw)
		if err != nil {
			return "", err
		}
		return AtIdentifier(did), nil
	}
	handle, err := ParseHandle(raw)
	if err != nil {
		return "", err
	}
	return AtIdentifier(handle), nil
}

func (n AtIdentifier) IsHandle() bool {
	return n != "" && !n.IsDID()
}

func (n AtIdentifier) AsHandle() (Handle, error) {
	if n.IsHandle() {
		return Handle(n), nil
	}
	return "", errors.New("AT Identifier is not a Handle")
}

func (n AtIdentifier) Handle() Handle {
	if n.IsHandle() {
		return Handle(n)
	}
	return ""
}

func (n AtIdentifier) IsDID() bool {
	return strings.HasPrefix(n.String(), "did:")
}

func (n AtIdentifier) AsDID() (DID, error) {
	if n.IsDID() {
		return DID(n), nil
	}
	return "", errors.New("AT Identifier is not a DID")
}

func (n AtIdentifier) DID() DID {
	if n.IsDID() {
		return DID(n)
	}
	return ""
}

func (n AtIdentifier) Normalize() AtIdentifier {
	if n.IsHandle() {
		return Handle(n).Normalize().AtIdentifier()
	}
	return n
}

func (n AtIdentifier) String() string {
	return string(n)
}

func (a AtIdentifier) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *AtIdentifier) UnmarshalText(text []byte) error {
	atid, err := ParseAtIdentifier(string(text))
	if err != nil {
		return err
	}
	*a = atid
	return nil
}
