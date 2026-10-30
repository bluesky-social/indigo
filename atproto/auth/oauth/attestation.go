package oauth

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
)

// NewClientAttestation signs a fresh, single-use proof of this confidential
// client's identity for a space authority. The KeyID must remain published in
// the client's JWKS for as long as tokens bound to that key remain valid.
func (cfg *ClientConfig) NewClientAttestation(authority syntax.DID) (string, error) {
	if !cfg.IsConfidential() || cfg.ClientID == "" || *cfg.KeyID == "" {
		return "", fmt.Errorf("client attestation requires a confidential client and key ID")
	}
	if _, err := syntax.ParseDID(authority.String()); err != nil {
		return "", fmt.Errorf("invalid space authority DID: %w", err)
	}
	method, err := keySigningMethod(cfg.PrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(method, jwt.RegisteredClaims{
		Issuer:    cfg.ClientID,
		Subject:   cfg.ClientID,
		Audience:  jwt.ClaimStrings{authority.String() + "#atproto_pds"},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(60 * time.Second)),
		ID:        secureRandomBase64(16),
	})
	token.Header["typ"] = "atproto-client-attestation+jwt"
	token.Header["kid"] = *cfg.KeyID
	return token.SignedString(cfg.PrivateKey)
}
