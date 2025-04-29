package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

// Returns an early-2024 timestamp as a point in time for validating known JWTs (which contain expires-at)
func testTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}

func validateMinimal(token string, iss, aud string, pub crypto.PublicKey) error {

	p := jwt.NewParser(
		jwt.WithValidMethods(supportedAlgs),
		jwt.WithTimeFunc(testTime),
		jwt.WithIssuer(iss),
		jwt.WithAudience(aud),
	)
	_, err := p.Parse(token, func(tok *jwt.Token) (any, error) {
		return pub, nil
	})
	if err != nil {
		return fmt.Errorf("failed to parse auth header JWT: %w", err)
	}
	return nil
}

func TestSignatureMethods(t *testing.T) {
	assert := assert.New(t)

	jwtTestFixtures := []struct {
		name   string
		pubkey string
		iss    string
		aud    string
		jwt    string
	}{
		{
			name:   "secp256k1 (K-256)",
			pubkey: "did:key:zQ3shscXNYZQZSPwegiv7uQZZV5kzATLBRtgJhs7uRY7pfSk4",
			iss:    "did:example:iss",
			aud:    "did:example:aud",
			jwt:    "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NksifQ.eyJpc3MiOiJkaWQ6ZXhhbXBsZTppc3MiLCJhdWQiOiJkaWQ6ZXhhbXBsZTphdWQiLCJleHAiOjE3MTM1NzEwMTJ9.J_In_PQCMjygeeoIKyjybORD89ZnEy1bZTd--sdq_78qv3KCO9181ZAh-2Pl0qlXZjfUlxgIa6wiak2NtsT98g",
		},
		{
			name:   "secp256k1 (K-256)",
			pubkey: "did:key:zQ3shqKrpHzQ5HDfhgcYMWaFcpBK3SS39wZLdTjA5GeakX8G5",
			iss:    "did:example:iss",
			aud:    "did:example:aud",
			jwt:    "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NksifQ.eyJhdWQiOiJkaWQ6ZXhhbXBsZTphdWQiLCJpc3MiOiJkaWQ6ZXhhbXBsZTppc3MiLCJleHAiOjE3MTM1NzExMzJ9.itNeYcF5oFMZIGxtnbJhE4McSniv_aR-Yk1Wj8uWk1K8YjlS2fzuJMo0-fILV3payETxn6r45f0FfpTaqY0EZQ",
		},
		{
			name:   "P-256",
			pubkey: "did:key:zDnaeXRDKRCEUoYxi8ZJS2pDsgfxUh3pZiu3SES9nbY4DoART",
			iss:    "did:example:iss",
			aud:    "did:example:aud",
			jwt:    "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJkaWQ6ZXhhbXBsZTppc3MiLCJhdWQiOiJkaWQ6ZXhhbXBsZTphdWQiLCJleHAiOjE3MTM1NzE1NTR9.FFRLm7SGbDUp6cL0WoCs0L5oqNkjCXB963TqbgI-KxIjbiqMQATVCalcMJx17JGTjMmfVHJP6Op_V4Z0TTjqog",
		},
	}

	for _, fix := range jwtTestFixtures {

		pubk, err := crypto.ParsePublicDIDKey(fix.pubkey)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(validateMinimal(fix.jwt, fix.iss, fix.aud, pubk))
	}
}

func testSigningValidation(t *testing.T, priv crypto.PrivateKey) {
	assert := assert.New(t)
	ctx := context.Background()

	iss := syntax.DID("did:example:iss")
	aud := "did:example:aud#svc"
	lxm := syntax.NSID("com.example.api")

	priv, err := crypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: iss,
		Keys: map[string]identity.Key{
			"atproto": identity.Key{
				Type:               "Multikey",
				PublicKeyMultibase: pub.Multibase(),
			},
		},
	})

	v := ServiceAuthValidator{
		Audience: aud,
		Dir:      &dir,
	}

	t1, err := SignServiceAuth(iss, aud, time.Minute, nil, priv)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := v.Validate(ctx, t1, nil)
	assert.NoError(err)
	assert.Equal(d1, iss)
	_, err = v.Validate(ctx, t1, &lxm)
	assert.Error(err)

	t2, err := SignServiceAuth(iss, aud, time.Minute, &lxm, priv)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := v.Validate(ctx, t2, nil)
	assert.NoError(err)
	assert.Equal(d2, iss)
	_, err = v.Validate(ctx, t2, &lxm)
	assert.NoError(err)

	_, err = v.Validate(ctx, t2, nil)
	assert.NoError(err)
	_, err = v.Validate(ctx, t2, &lxm)
	assert.NoError(err)
}

func TestP256SigningValidation(t *testing.T) {
	priv, err := crypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	testSigningValidation(t, priv)
}

func TestK256SigningValidation(t *testing.T) {
	priv, err := crypto.GeneratePrivateKeyK256()
	if err != nil {
		t.Fatal(err)
	}
	testSigningValidation(t, priv)
}
