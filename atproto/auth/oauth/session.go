package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-querystring/query"
)

type RefreshCallback = func(ctx context.Context, data SessionData)

type Session struct {
	// HTTP client used for token refresh requests
	Client *http.Client

	Config         *ClientConfig
	Data           *SessionData
	DpopPrivateKey crypto.PrivateKey

	RefreshCallback RefreshCallback

	// Lock which protects concurrent access to session data (eg, access and refresh tokens)
	lk sync.RWMutex
}

func (sess *Session) RefreshTokens(ctx context.Context) error {

	// TODO: assuming confidential client
	clientAssertion, err := sess.Config.NewAssertionJWT(sess.Data.AuthServerURL)
	if err != nil {
		return err
	}

	body := RefreshTokenRequest{
		ClientID:            sess.Config.ClientID,
		GrantType:           "authorization_code",
		RefreshToken:        sess.Data.RefreshToken,
		ClientAssertionType: &CLIENT_ASSERTION_JWT_BEARER,
		ClientAssertion:     &clientAssertion,
	}

	vals, err := query.Values(body)
	if err != nil {
		return err
	}
	bodyBytes := []byte(vals.Encode())

	// XXX: persist this back to the data?
	dpopServerNonce := sess.Data.DpopAuthServerNonce
	tokenURL := sess.Data.AuthServerTokenEndpoint

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewDPoPJWT("POST", tokenURL, dpopServerNonce, sess.DpopPrivateKey)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = sess.Client.Do(req)
		if err != nil {
			return err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("initial token request failed", "authServer", tokenURL, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("initial token request failed", "authServer", tokenURL, "resp", errResp, "statusCode", resp.StatusCode)
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
			slog.Warn("initial token request failed", "authServer", tokenURL, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("initial token request failed", "authServer", tokenURL, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return fmt.Errorf("initial token request failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("token response failed to decode: %w", err)
	}
	// XXX: more validation of response?

	sess.Data.AccessToken = tokenResp.AccessToken
	sess.Data.RefreshToken = tokenResp.RefreshToken

	return nil
}

func (sess *Session) NewAccessDPoP(method, reqURL string) (string, error) {

	ath := S256CodeChallenge(sess.Data.AccessToken)
	claims := dpopClaims{
		HTTPMethod:      method,
		TargetURI:       reqURL,
		AccessTokenHash: &ath,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    sess.Data.AuthServerURL,
			ID:        randomNonce(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(JWT_EXPIRATION_DURATION)),
		},
	}
	if sess.Data.DpopHostNonce != "" {
		claims.Nonce = &sess.Data.DpopHostNonce
	}

	keyMethod, err := keySigningMethod(sess.DpopPrivateKey)
	if err != nil {
		return "", err
	}

	// TODO: parse/cache this elsewhere
	pub, err := sess.DpopPrivateKey.PublicKey()
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
	return token.SignedString(sess.DpopPrivateKey)
}

func (sess *Session) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {

	// XXX: copy URL and strip query params
	u := req.URL.String()

	dpopServerNonce := sess.Data.DpopHostNonce
	var resp *http.Response
	for range 2 {
		dpopJWT, err := sess.NewAccessDPoP(req.Method, u)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("DPoP %s", sess.Data.AccessToken))
		req.Header.Set("DPoP", dpopJWT)

		resp, err = c.Do(req)
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
