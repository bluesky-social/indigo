package spaces

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/golang-jwt/jwt/v5"
)

// ErrMissingCredential indicates an authenticated operation on a resolution-only handle.
var ErrMissingCredential = errors.New("space operation requires a credential")

// ErrInvalidCredentialResponse reports an empty or header-invalid issued token.
var ErrInvalidCredentialResponse = errors.New("space authority returned an invalid credential")

// Credential is an immutable issued token and its bound P-256 signer. The signer
// may be opaque/nonexportable and must remain usable for the credential's lifetime.
// Construction does not validate the issuer's JWT signature or contact a service.
type Credential struct {
	token  string
	signer atcrypto.PrivateKey
}

// NewCredential creates a credential from an issued token and its bound P-256 signer.
// It does not verify the token signature or contact a service.
func NewCredential(token string, signer atcrypto.PrivateKey) (*Credential, error) {
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("space credential token is empty or contains a newline")
	}
	_, err := credentialKeyID(signer)
	if err != nil {
		return nil, err
	}
	return &Credential{token: token, signer: signer}, nil
}

func (cred *Credential) Token() string {
	return cred.token
}

func credentialKeyID(signer atcrypto.PrivateKey) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("space credential requires a P-256 signer")
	}
	pub, err := signer.PublicKey()
	if err != nil {
		return "", fmt.Errorf("getting credential public key: %w", err)
	}
	if pub == nil {
		return "", fmt.Errorf("space credential requires a P-256 public key")
	}
	// Inspect the public encoding, not the concrete private key type: hardware
	// signers and other opaque PrivateKey implementations work the same way.
	keyID := pub.DIDKey()
	parsed, err := atcrypto.ParsePublicDIDKey(keyID)
	if err != nil {
		return "", fmt.Errorf("parsing credential public key: %w", err)
	}
	if _, ok := parsed.(*atcrypto.PublicKeyP256); !ok {
		return "", fmt.Errorf("space credential requires a P-256 signer")
	}
	return keyID, nil
}

// credentialAuth is request-scoped. It never replaces the handle's credential.
type credentialAuth struct {
	credential *Credential
	audience   syntax.DID
}

var _ atclient.AuthMethod = (*credentialAuth)(nil)

// DoWithAuth implements [atclient.AuthMethod] for a space credential. It signs
// the authorization value and audience with the protocol's required tag.
func (a *credentialAuth) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {
	authorization := "Atproto-Space-Credential " + a.credential.token
	const params = `("authorization" "atproto-space-audience");tag="atproto-space-credential"`
	base := "\"authorization\": " + authorization + "\n\"atproto-space-audience\": " + a.audience.String() + "\n\"@signature-params\": " + params
	sig, err := a.credential.signer.HashAndSign([]byte(base))
	if err != nil {
		return nil, fmt.Errorf("signing space authorization: %w", err)
	}
	if len(sig) != 64 {
		return nil, fmt.Errorf("space HTTP signature must be 64 bytes, got %d", len(sig))
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Atproto-Space-Audience", a.audience.String())
	req.Header.Set("Signature-Input", "atproto-space-credential="+params)
	req.Header.Set("Signature", "atproto-space-credential=:"+base64.StdEncoding.EncodeToString(sig)+":")
	return c.Do(req)
}

// GetDelegationToken explicitly obtains a short-lived delegation through an
// existing OAuth-authenticated PDS client. It does not create a space handle.
func GetDelegationToken(ctx context.Context, pds lexutil.LexClient, ref syntax.SpaceRef) (string, error) {
	out, err := comatproto.SpaceGetDelegationToken(ctx, pds, ref.String())
	if err != nil {
		return "", err
	}
	return out.Token, nil
}

// ExchangeCredential issues a new credential using the caller's fresh signer.
// Existing credentials and handles are unchanged. No renewal is scheduled.
func (c *Client) ExchangeCredential(ctx context.Context, ref syntax.SpaceRef, delegationToken string, signer atcrypto.PrivateKey, opts CredentialExchangeOptions) (*Credential, error) {
	if strings.ContainsAny(delegationToken, "\r\n") {
		return nil, fmt.Errorf("delegation token contains a newline")
	}
	keyID, err := credentialKeyID(signer)
	if err != nil {
		return nil, err
	}
	host, err := c.ResolveSpaceHost(ctx, ref)
	if err != nil {
		return nil, err
	}
	rpc := c.apiClient(host, nil)
	// Issuance is unauthenticated at the HTTP layer. Assertions and the key to
	// bind are explicit body fields, including when no assertion is required.
	rpc.Headers = rpc.Headers.Clone()
	rpc.Headers.Del("Authorization")
	rpc.Headers.Del("Signature-Input")
	rpc.Headers.Del("Signature")
	rpc.Headers.Del("Atproto-Space-Audience")
	var delegation *string
	if delegationToken != "" {
		delegation = &delegationToken
	}
	out, err := comatproto.SpaceGetSpaceCredential(ctx, rpc, &comatproto.SpaceGetSpaceCredential_Input{
		Space: ref.String(), CredentialKid: keyID, DelegationToken: delegation,
		ClientAttestation: opts.ClientAttestation,
	})
	if err != nil {
		return nil, err
	}
	// Keep the already-validated signer and key binding; no second resolution
	// or issuance is needed to construct the returned immutable credential.
	if out.Credential == "" || strings.ContainsAny(out.Credential, "\r\n") {
		return nil, ErrInvalidCredentialResponse
	}
	return &Credential{token: out.Credential, signer: signer}, nil
}

// RegisterNotify registers a service DID, optionally with a service fragment,
// and returns its expiry. The caller owns subscription renewal policy.
func (s *Space) RegisterNotify(ctx context.Context, service string) (syntax.Datetime, error) {
	if err := validateNotifyService(service); err != nil {
		return "", err
	}
	rpc, err := s.authenticatedClient(ctx)
	if err != nil {
		return "", err
	}
	out, err := comatproto.SpaceRegisterNotify(ctx, rpc, &comatproto.SpaceRegisterNotify_Input{
		Space: s.ref.String(), Service: service,
	})
	if err != nil {
		return "", err
	}
	expiry, err := syntax.ParseDatetime(out.ExpiresAt)
	if err != nil {
		return "", fmt.Errorf("invalid notification registration expiry: %w", err)
	}
	return expiry, nil
}

func (s *Space) UnregisterNotify(ctx context.Context, service string) error {
	if err := validateNotifyService(service); err != nil {
		return err
	}
	rpc, err := s.authenticatedClient(ctx)
	if err != nil {
		return err
	}
	return comatproto.SpaceUnregisterNotify(ctx, rpc, &comatproto.SpaceUnregisterNotify_Input{
		Space: s.ref.String(), Service: service,
	})
}

func validateNotifyService(service string) error {
	parts := strings.SplitN(service, "#", 3)
	if _, err := syntax.ParseDID(parts[0]); err != nil {
		return fmt.Errorf("notification service must be a DID with an optional fragment: %w", err)
	}
	if len(parts) > 2 || (len(parts) == 2 && parts[1] == "") {
		return fmt.Errorf("notification service has an invalid fragment")
	}
	return nil
}

// VerifyNotificationAuth verifies a service JWT's audience and endpoint using
// the existing validator, then requires the issuer expected for this notification.
// It does not receive notifications or perform any deletion or recovery action.
func VerifyNotificationAuth(ctx context.Context, validator *auth.ServiceAuthValidator, token string, endpoint syntax.NSID, expectedIssuer syntax.DID) error {
	if validator == nil || validator.Dir == nil || validator.Audience == "" {
		return fmt.Errorf("notification auth requires a validator with directory and audience")
	}
	if _, err := syntax.ParseDID(expectedIssuer.String()); err != nil {
		return fmt.Errorf("%w: invalid expected notification issuer: %w", jwt.ErrTokenInvalidIssuer, err)
	}
	issuer, err := validator.Validate(ctx, token, &endpoint)
	if err != nil {
		return err
	}
	if issuer != expectedIssuer {
		return fmt.Errorf("%w: notification issuer %s, expected %s", jwt.ErrTokenInvalidIssuer, issuer, expectedIssuer)
	}
	return nil
}
