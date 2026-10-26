package oauth

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/golang-jwt/jwt/v5"
)

func TestClientAttestationIdentityAudienceAndFreshness(t *testing.T) {
	priv, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewPublicConfig("https://app.example/client-metadata.json", "https://app.example/callback", []string{"atproto"})
	if _, err := cfg.NewClientAttestation("did:plc:authority"); err == nil {
		t.Fatal("public client signed an attestation")
	}
	if err := cfg.SetClientSecret(priv, "current"); err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for range 2 {
		raw, err := cfg.NewClientAttestation("did:plc:authority")
		if err != nil {
			t.Fatal(err)
		}
		claims := &jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) { return pub, nil }, jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer(cfg.ClientID), jwt.WithSubject(cfg.ClientID), jwt.WithAudience("did:plc:authority#atproto_pds"), jwt.WithExpirationRequired())
		if err != nil {
			t.Fatal(err)
		}
		if !token.Valid || token.Header["typ"] != "atproto-client-attestation+jwt" || token.Header["kid"] != "current" {
			t.Fatal("wrong attestation header")
		}
		if claims.ExpiresAt.Sub(claims.IssuedAt.Time) != time.Minute {
			t.Fatal("attestation lifetime is not 60 seconds")
		}
		if claims.ID == "" || seen[claims.ID] {
			t.Fatal("attestation jti was reused")
		}
		seen[claims.ID] = true
	}
	jwks := cfg.PublicJWKS()
	if len(jwks.Keys) != 1 || *jwks.Keys[0].KeyID != "current" {
		t.Fatal("signing key is not published")
	}
	if _, err := cfg.NewClientAttestation("did:plc:authority#fragment"); err == nil {
		t.Fatal("non-DID authority accepted")
	}
}
