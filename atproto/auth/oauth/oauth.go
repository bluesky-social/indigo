package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-querystring/query"
)

type ClientConfig struct {
	ClientID   string
	PrivateKey crypto.PrivateKey
	KeyID      string

	// TODO: ClientMetadata() method, with required fields?
	// TODO: JWKS() method, returns public keys?
}

type Session struct {
	Config ClientConfig
	Data   SessionData
}

type OAuthClient struct {
	Client              *http.Client
	ClientID            string
	Resolver            *Resolver
	TTL                 time.Duration
	DpopSecretKey       crypto.PrivateKey
	DpopSecretMultibase string
	ClientSecretKey     crypto.PrivateKey
	//HostURL string
	AuthServerURL string

	// XXX: hack
	Session *SessionData
}

func NewOAuthClient(clientID string) OAuthClient {
	// TODO: include SSRF protections on http.Client{} by default
	c := OAuthClient{
		Client:   http.DefaultClient,
		ClientID: clientID,
		Resolver: NewResolver(),
		TTL:      30 * time.Second,
	}
	return c
}

type clientAssertionClaims struct {
	jwt.RegisteredClaims

	HTTPMethod      string  `json:"htm"`
	TargetURI       string  `json:"hti"`
	AccessTokenHash *string `json:"ath,omitempty"`
	Nonce           *string `json:"nonce,omitempty"`
}

type dpopClaims struct {
	jwt.RegisteredClaims

	HTTPMethod      string  `json:"htm"`
	TargetURI       string  `json:"htu"`
	AccessTokenHash *string `json:"ath,omitempty"`
	Nonce           *string `json:"nonce,omitempty"`
}

// TODO: params are just fields on OAuthClient
func (c *OAuthClient) NewClientAssertionJWT(clientID, authURL string, clientSecretKey crypto.PrivateKey, kid string) (string, error) {
	claims := clientAssertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   c.ClientID,
			Subject:  c.ClientID,
			Audience: []string{authURL},
			ID:       randomNonce(),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}

	var keyMethod jwt.SigningMethod
	switch clientSecretKey.(type) {
	case *crypto.PrivateKeyP256:
		keyMethod = signingMethodES256
	case *crypto.PrivateKeyK256:
		keyMethod = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown clientSecretKey type")
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	token.Header["kid"] = kid
	return token.SignedString(clientSecretKey)
}

func (c *OAuthClient) NewDPoPJWT(httpMethod, url, dpopNonce string) (string, error) {

	claims := dpopClaims{
		HTTPMethod: httpMethod,
		TargetURI:  url,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        randomNonce(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(c.TTL)),
		},
	}
	if dpopNonce != "" {
		claims.Nonce = &dpopNonce
	}

	// XXX: refactor to helper method
	var keyMethod jwt.SigningMethod
	switch c.DpopSecretKey.(type) {
	case *crypto.PrivateKeyP256:
		keyMethod = signingMethodES256
	case *crypto.PrivateKeyK256:
		keyMethod = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown clientSecretKey type")
	}

	// XXX: parse/cache this elsewhere
	pub, err := c.DpopSecretKey.PublicKey()
	if err != nil {
		return "", err
	}
	pubJWK, err := pub.JWK()
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	token.Header["typ"] = "dpop+jwt"
	token.Header["jwk"] = pubJWK
	return token.SignedString(c.DpopSecretKey)
}

// Sends PAR request to auth server
func (c *OAuthClient) SendAuthRequest(ctx context.Context, authMeta *AuthServerMetadata, redirectURI, loginHint, scope string) (*AuthRequestData, error) {
	parURL := authMeta.PushedAuthorizationRequestEndpoint
	state := randomNonce()
	pkceVerifier := fmt.Sprintf("%s%s%s", randomNonce(), randomNonce(), randomNonce())

	// generate PKCE code challenge for use in PAR request
	codeChallenge := S256CodeChallenge(pkceVerifier)

	// self-signed JWT using private key in client metadata (confidential client)
	// TODO: make "confidential client" mode optional
	assertionJWT, err := c.NewClientAssertionJWT(c.ClientID, authMeta.Issuer, c.ClientSecretKey, "one") // XXX: keyID
	if err != nil {
		return nil, err
	}

	body := PushedAuthRequest{
		ClientID:            c.ClientID,
		State:               state,
		RedirectURI:         redirectURI,
		Scope:               scope,
		ResponseType:        "code",
		ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		ClientAssertion:     assertionJWT,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	}
	if loginHint != "" {
		body.LoginHint = &loginHint
	}
	vals, err := query.Values(body)
	if err != nil {
		return nil, err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := ""

	slog.Info("sending auth request", "scope", scope, "state", state, "redirectURI", redirectURI)

	var resp *http.Response
	for range 2 {
		dpopJWT, err := c.NewDPoPJWT("POST", parURL, dpopServerNonce)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", parURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = c.Client.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("PAR request failed", "authServer", parURL, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("PAR request failed", "authServer", parURL, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			slog.Warn("PAR request failed", "authServer", parURL, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("PAR request failed", "authServer", parURL, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return nil, fmt.Errorf("auth request (PAR) failed: HTTP %d", resp.StatusCode)
	}

	var parResp PushedAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&parResp); err != nil {
		return nil, fmt.Errorf("auth request (PAR) response failed to decode: %w", err)
	}

	parInfo := AuthRequestData{
		State:         state,
		AuthServerURL: c.AuthServerURL,
		//XXX: HostURL
		Scope:               scope,
		PKCEVerifier:        pkceVerifier,
		RequestURI:          parResp.RequestURI,
		DpopAuthServerNonce: dpopServerNonce,
		DpopKeyMultibase:    c.DpopSecretMultibase,
	}

	return &parInfo, nil
}

func ResumeAuthRequest(clientID string, info *AuthRequestData) (*OAuthClient, error) {

	priv, err := crypto.ParsePrivateMultibase(info.DpopKeyMultibase)
	if err != nil {
		return nil, err
	}

	c := NewOAuthClient(clientID)
	c.DpopSecretKey = priv
	//XXX ClientSecretKey
	//XXX HostURL: info.HostURL,
	c.AuthServerURL = info.AuthServerURL
	return &c, nil
}

func (c *OAuthClient) SendInitialTokenRequest(ctx context.Context, authCode string, info AuthRequestData, redirectURI string) (*TokenResponse, error) {

	clientAssertion, err := c.NewClientAssertionJWT(c.ClientID, c.AuthServerURL, c.ClientSecretKey, "one") // XXX: keyID
	if err != nil {
		return nil, err
	}

	// TODO: don't re-fetch? caching?
	authServerMeta, err := c.Resolver.ResolveAuthServerMetadata(ctx, c.AuthServerURL)
	if err != nil {
		return nil, err
	}

	body := InitialTokenRequest{
		ClientID:            c.ClientID,
		RedirectURI:         redirectURI,
		GrantType:           "authorization_code",
		Code:                authCode,
		CodeVerifier:        info.PKCEVerifier,
		ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		ClientAssertion:     clientAssertion,
	}

	vals, err := query.Values(body)
	if err != nil {
		return nil, err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := info.DpopAuthServerNonce

	var resp *http.Response
	for range 2 {
		dpopJWT, err := c.NewDPoPJWT("POST", authServerMeta.TokenEndpoint, dpopServerNonce)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", authServerMeta.TokenEndpoint, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = c.Client.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return nil, fmt.Errorf("initial token request failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token response failed to decode: %w", err)
	}

	return &tokenResp, nil
}

func (c *OAuthClient) RefreshTokens(ctx context.Context, sess *SessionData) error {

	clientAssertion, err := c.NewClientAssertionJWT(c.ClientID, sess.AuthServerURL, c.ClientSecretKey, "one") // XXX: keyID
	if err != nil {
		return err
	}

	// TODO: don't re-fetch? caching?
	authServerMeta, err := c.Resolver.ResolveAuthServerMetadata(ctx, sess.AuthServerURL)
	if err != nil {
		return err
	}

	body := RefreshTokenRequest{
		ClientID:            c.ClientID,
		GrantType:           "authorization_code",
		RefreshToken:        sess.RefreshToken,
		ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		ClientAssertion:     clientAssertion,
	}

	vals, err := query.Values(body)
	if err != nil {
		return err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := sess.DpopAuthServerNonce

	var resp *http.Response
	for range 2 {
		dpopJWT, err := c.NewDPoPJWT("POST", authServerMeta.TokenEndpoint, dpopServerNonce)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", authServerMeta.TokenEndpoint, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = c.Client.Do(req)
		if err != nil {
			return err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return fmt.Errorf("initial token request failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("token response failed to decode: %w", err)
	}
	// XXX: more validation of response?

	sess.AccessToken = tokenResp.AccessToken
	sess.RefreshToken = tokenResp.RefreshToken

	return nil
}

func (c *OAuthClient) NewPDSDPoPJWT(httpMethod, url, dpopNonce, accessToken string) (string, error) {

	ath := S256CodeChallenge(accessToken)
	claims := dpopClaims{
		HTTPMethod:      httpMethod,
		TargetURI:       url,
		AccessTokenHash: &ath,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.AuthServerURL,
			ID:        randomNonce(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(c.TTL)),
		},
	}
	if dpopNonce != "" {
		claims.Nonce = &dpopNonce
	}

	// XXX: refactor to helper method
	var keyMethod jwt.SigningMethod
	switch c.DpopSecretKey.(type) {
	case *crypto.PrivateKeyP256:
		keyMethod = signingMethodES256
	case *crypto.PrivateKeyK256:
		keyMethod = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown clientSecretKey type")
	}

	// XXX: parse/cache this elsewhere
	pub, err := c.DpopSecretKey.PublicKey()
	if err != nil {
		return "", err
	}
	pubJWK, err := pub.JWK()
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	token.Header["typ"] = "dpop+jwt"
	token.Header["jwk"] = pubJWK
	return token.SignedString(c.DpopSecretKey)
}

func (c *OAuthClient) DoWithAuth(req *http.Request, httpClient *http.Client) (*http.Response, error) {

	sess := c.Session
	u := req.URL.String()
	dpopServerNonce := sess.DpopAuthServerNonce

	var resp *http.Response
	for range 2 {
		dpopJWT, err := c.NewPDSDPoPJWT("POST", u, dpopServerNonce, sess.AccessToken)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("DPoP %s", sess.AccessToken))
		req.Header.Set("DPoP", dpopJWT)

		resp, err = httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("authorized request failed", "url", u, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("authorized request failed", "url", u, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// XXX: doesn't really work, body might be drained second time
			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}
	// TODO: check for auth-specific errors, and return them as err
	return resp, nil
}
