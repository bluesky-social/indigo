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

func (a *credentialAuth) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {
	authorization := "Atproto-Space " + a.credential.token
	params := `("authorization" "atproto-space-audience")`
	// RFC 9421 components are ordered exactly as in the proposal, separated by
	// actual newlines (not the two literal characters backslash and n).
	base := "\"authorization\": " + authorization + "\n\"atproto-space-audience\": " + a.audience.String() + "\n\"@signature-params\": " + params
	if err := signRequest(req, a.credential.signer, authorization, params, base); err != nil {
		return nil, err
	}
	req.Header.Set("Atproto-Space-Audience", a.audience.String())
	return c.Do(req)
}

type delegationAuth struct {
	token  string
	signer atcrypto.PrivateKey
	keyID  string
}

var _ atclient.AuthMethod = (*delegationAuth)(nil)

func (a *delegationAuth) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {
	authorization := "Bearer " + a.token
	params := `("authorization");keyid="` + a.keyID + `"`
	base := "\"authorization\": " + authorization + "\n\"@signature-params\": " + params
	if err := signRequest(req, a.signer, authorization, params, base); err != nil {
		return nil, err
	}
	// Exchange covers only Authorization, even when client defaults contain
	// an audience header from an unrelated request.
	req.Header.Del("Atproto-Space-Audience")
	return c.Do(req)
}

func signRequest(req *http.Request, signer atcrypto.PrivateKey, authorization, params, base string) error {
	sig, err := signer.HashAndSign([]byte(base))
	if err != nil {
		return fmt.Errorf("signing space authorization: %w", err)
	}
	if len(sig) != 64 {
		return fmt.Errorf("space HTTP signature must be 64 bytes, got %d", len(sig))
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Signature-Input", "atproto-space="+params)
	req.Header.Set("Signature", "atproto-space=:"+base64.StdEncoding.EncodeToString(sig)+":")
	return nil
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
	if delegationToken == "" || strings.ContainsAny(delegationToken, "\r\n") {
		return nil, fmt.Errorf("delegation token is empty or contains a newline")
	}
	keyID, err := credentialKeyID(signer)
	if err != nil {
		return nil, err
	}
	host, err := c.ResolveSpaceHost(ctx, ref)
	if err != nil {
		return nil, err
	}
	rpc := c.apiClient(host, &delegationAuth{token: delegationToken, signer: signer, keyID: keyID})
	out, err := comatproto.SpaceGetSpaceCredential(ctx, rpc, &comatproto.SpaceGetSpaceCredential_Input{
		Space: ref.String(), ClientAttestation: opts.ClientAttestation,
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
