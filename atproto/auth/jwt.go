package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
)

// TODO: check for uniqueness of JTI (random nonce) to prevent token replay

type ServiceAuthValidator struct {
	// Service DID reference for this validator: a DID with optional #-separated fragment
	Audience string
	Dir      identity.Directory
}

type serviceAuthClaims struct {
	jwt.RegisteredClaims

	LexMethod string `json:"lxm,omitempty"`
}

func (s *ServiceAuthValidator) Validate(ctx context.Context, tokenString string, lexMethod *syntax.NSID) (syntax.DID, error) {

	opts := []jwt.ParserOption{
		jwt.WithValidMethods(supportedAlgs),
		jwt.WithAudience(s.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(5 * time.Second), // TODO: configurable? better default?
	}

	token, err := jwt.ParseWithClaims(tokenString, &serviceAuthClaims{}, s.fetchIssuerKeyFunc(ctx), opts...)
	if err != nil && errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		// if signature validation fails, purge the directory and try again
		// TODO: probably need to cache or rate-limit this?

		// do an unvalidated extraction of 'iss' from JWT
		insecure := jwt.NewParser(jwt.WithoutClaimsValidation())
		t, _, err := insecure.ParseUnverified(tokenString, &jwt.MapClaims{})
		claims, ok := t.Claims.(*jwt.MapClaims)
		if !ok {
			return "", jwt.ErrTokenInvalidClaims
		}
		iss, err := claims.GetIssuer()
		if err != nil {
			return "", err
		}
		did, err := syntax.ParseDID(iss)
		if err != nil {
			return "", fmt.Errorf("%w: invalid DID: %w", jwt.ErrTokenInvalidIssuer, err)
		}

		slog.Info("purging directory and retrying service auth signature validation", "did", did)
		err = s.Dir.Purge(ctx, did.AtIdentifier())
		if err != nil {
			slog.Error("purging identity directory", "did", did, "err", err)
		}
		token, err = jwt.ParseWithClaims(tokenString, &serviceAuthClaims{}, s.fetchIssuerKeyFunc(ctx), opts...)
	}
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*serviceAuthClaims)
	if !ok {
		// TODO: is this the best error here?
		return "", jwt.ErrTokenInvalidClaims
	}

	if lexMethod != nil && claims.LexMethod != lexMethod.String() {
		return "", fmt.Errorf("%w: Lexicon endpoint (LXM)", jwt.ErrTokenInvalidClaims)
	}

	// NOTE: KeyFunc has already parsed issuer, so we know it is a valid DID
	did := syntax.DID(claims.Issuer)
	return did, nil
}

// resolves public key from identity directory
func (s *ServiceAuthValidator) fetchIssuerKeyFunc(ctx context.Context) func(token *jwt.Token) (any, error) {
	return func(token *jwt.Token) (any, error) {
		claims, ok := token.Claims.(*serviceAuthClaims)
		if !ok {
			return nil, jwt.ErrTokenInvalidClaims
		}
		iss, err := claims.GetIssuer()
		if err != nil {
			return nil, fmt.Errorf("%w: missing 'iss' claim", jwt.ErrTokenInvalidIssuer)
		}
		did, err := syntax.ParseDID(iss)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid DID: %w", jwt.ErrTokenInvalidIssuer, err)
		}
		// NOTE: this will do handle resolution by default
		ident, err := s.Dir.LookupDID(ctx, did)
		if err != nil {
			return nil, fmt.Errorf("%w: resolving DID (%s): %w", jwt.ErrTokenInvalidIssuer, did, err)
		}
		return ident.PublicKey()
	}
}

func randomNonce() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func SignServiceAuth(iss syntax.DID, aud string, ttl time.Duration, lexMethod *syntax.NSID, priv crypto.PrivateKey) (string, error) {
	claims := serviceAuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    iss.String(),
			Audience:  []string{aud},
			ID:        randomNonce(),
		},
	}
	if lexMethod != nil {
		claims.LexMethod = lexMethod.String()
	}

	var sm *signingMethodAtproto

	// NOTE: could also have a crypto.PrivateKey.Alg() method which returns a string
	switch priv.(type) {
	case *crypto.PrivateKeyP256:
		sm = signingMethodES256
	case *crypto.PrivateKeyK256:
		sm = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown signing key type: %T", priv)
	}

	token := jwt.NewWithClaims(sm, claims)
	return token.SignedString(priv)
}
