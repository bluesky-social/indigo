package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DIDDocument struct {
	DID                syntax.DID              `json:"id"`
	AlsoKnownAs        []string                `json:"alsoKnownAs,omitempty"`
	VerificationMethod []DocVerificationMethod `json:"alsoKnownAs,omitempty"`
	Service            []DocService            `json:"alsoKnownAs,omitempty"`
}

type DocVerificationMethod struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Controller         string `json:"controller"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
}

type DocService struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

var ErrDIDNotFound = errors.New("DID not found")

// WARNING: this does *not* bi-directionally verify account metadata; it only implements direct DID-to-DID-document lookup for the supported DID methods, and parses the resulting DID Doc into an Account struct
func ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	switch did.Method() {
	case "web":
		panic("NOT IMPLEMENTED")
	case "plc":
		panic("NOT IMPLEMENTED")
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}

func ResolveDIDWeb(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	return nil, fmt.Errorf("XXX UNIMPLEMENTED")
}

func ResolvePLC(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	return nil, fmt.Errorf("XXX UNIMPLEMENTED")
}

func (d *DIDDocument) Account() Account {
	panic("XXX UNIMPLEMENTED")
}

// "Renders" a DID Document
func (a *Account) DIDDocument() DIDDocument {
	panic("XXX UNIMPLEMENTED")
}
