package identity

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DIDDocument struct {
	DID                syntax.DID              `json:"id"`
	AlsoKnownAs        []string                `json:"alsoKnownAs,omitempty"`
	VerificationMethod []DocVerificationMethod `json:"verificationMethod,omitempty"`
	Service            []DocService            `json:"service,omitempty"`
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
