package identity

import (
	"encoding/json"

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

// DID Core allows `serviceEndpoint` to be a string, a map, or a set. atproto only uses string endpoints, so services with any other kind of endpoint are dropped while parsing, instead of failing the entire document.
func (d *DIDDocument) UnmarshalJSON(b []byte) error {
	type rawService struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		ServiceEndpoint any    `json:"serviceEndpoint"`
	}
	// alias type has the same fields, but not this method (which would recurse)
	type alias DIDDocument
	raw := struct {
		*alias
		Service []rawService `json:"service"`
	}{alias: (*alias)(d)}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	d.Service = nil
	for _, s := range raw.Service {
		endpoint, ok := s.ServiceEndpoint.(string)
		if !ok {
			continue
		}
		d.Service = append(d.Service, DocService{ID: s.ID, Type: s.Type, ServiceEndpoint: endpoint})
	}
	return nil
}
