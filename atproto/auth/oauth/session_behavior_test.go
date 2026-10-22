package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type behaviorTransport func(*http.Request) (*http.Response, error)

func (f behaviorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func behaviorResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type behaviorStore struct {
	*MemStore
	saveErr    error
	requestErr error
}

func (s *behaviorStore) SaveSession(ctx context.Context, data ClientSessionData) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	return s.MemStore.SaveSession(ctx, data)
}
func (s *behaviorStore) SaveAuthRequestInfo(ctx context.Context, data AuthRequestData) error {
	if s.requestErr != nil {
		return s.requestErr
	}
	return s.MemStore.SaveAuthRequestInfo(ctx, data)
}

func behaviorApp(t *testing.T, transport behaviorTransport) (*ClientApp, *behaviorStore, ClientSessionData) {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	store := &behaviorStore{MemStore: NewMemStore()}
	cfg := NewPublicConfig("https://app.example/client.json", "https://app.example/callback", []string{"atproto"})
	app := NewClientApp(&cfg, store)
	app.Client = &http.Client{Transport: transport}
	data := ClientSessionData{AccountDID: "did:plc:user", SessionID: "session", HostURL: "https://pds.example", AuthServerURL: "https://pds.example", AuthServerTokenEndpoint: "https://pds.example/token", AuthServerRevocationEndpoint: "https://pds.example/revoke", AccessToken: "old-access", RefreshToken: "old-refresh", DPoPPrivateKeyMultibase: key.Multibase(), Scopes: []string{"atproto", "space:*?authority=*"}}
	if err := store.SaveSession(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	return app, store, data
}

func TestConcurrentResumesAndStale401RefreshOnlyOnce(t *testing.T) {
	var stale, refreshes atomic.Int32
	ready := make(chan struct{})
	app, store, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			if refreshes.Add(1) != 1 {
				return behaviorResponse(400, `{"error":"invalid_grant"}`), nil
			}
			return behaviorResponse(200, `{"access_token":"new-access","refresh_token":"new-refresh","scope":"atproto space:*?authority=*&action=read_self"}`), nil
		}
		if r.Header.Get("Authorization") == "DPoP old-access" {
			if stale.Add(1) == 2 {
				close(ready)
			}
			<-ready
			resp := behaviorResponse(401, "")
			resp.Header.Set("WWW-Authenticate", `DPoP error="invalid_token"`)
			return resp, nil
		}
		return behaviorResponse(200, "success"), nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
			if err != nil {
				errs <- err
				return
			}
			req, _ := http.NewRequest("GET", data.HostURL+"/resource", nil)
			resp, err := sess.DoWithAuth(app.Client, req, syntax.NSID("com.example.read"))
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode != 200 {
					err = errors.New("resource request failed")
				}
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshed %d times", refreshes.Load())
	}
	saved, err := store.GetSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "new-refresh" || len(saved.Scopes) != 2 || !strings.Contains(saved.Scopes[1], "read_self") {
		t.Fatal("rotated tokens and reduced consent were not persisted")
	}
}

func TestRefreshPersistenceFailureIsReturnedWithoutReusingRotatedToken(t *testing.T) {
	app, store, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		return behaviorResponse(200, `{"access_token":"new-access","refresh_token":"new-refresh","scope":"atproto"}`), nil
	})
	sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("disk unavailable")
	store.saveErr = failure
	if _, err := sess.RefreshTokens(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("persistence failure not returned: %v", err)
	}
	live := sess.SessionData()
	if live.RefreshToken != "new-refresh" || len(live.Scopes) != 1 {
		t.Fatal("lost rotated tokens or refreshed scope")
	}
}

func TestLostNoncePersistenceDoesNotFailResourceRequest(t *testing.T) {
	var calls atomic.Int32
	app, store, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			resp := behaviorResponse(401, "")
			resp.Header.Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			resp.Header.Set("DPoP-Nonce", "fresh-nonce")
			return resp, nil
		}
		return behaviorResponse(200, "success"), nil
	})
	sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	store.saveErr = errors.New("disk unavailable")
	req, _ := http.NewRequest("GET", data.HostURL+"/resource", nil)
	resp, err := sess.DoWithAuth(app.Client, req, "com.example.read")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || calls.Load() != 2 {
		t.Fatal("lost nonce persistence stopped request retry")
	}
	if err := sess.UpdateHostDPoPNonce(context.Background(), "another"); !errors.Is(err, store.saveErr) {
		t.Fatal("nonce persistence error not returned")
	}
}

func TestTokenEndpointRevocationAndTimeoutAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *http.Response
		cause    error
		revoked  bool
	}{
		{"revoked", behaviorResponse(400, `{"error":"invalid_grant","error_description":"private-token"}`), nil, true},
		{"timeout", nil, context.DeadlineExceeded, false},
		{"unavailable", behaviorResponse(503, `{"error":"server_error","error_description":"private-token"}`), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) { return tc.response, tc.cause })
			sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = sess.RefreshTokens(context.Background())
			var endpointErr *TokenEndpointError
			if !errors.As(err, &endpointErr) {
				t.Fatalf("not a typed token endpoint error: %v", err)
			}
			if errors.Is(err, ErrGrantRevoked) != tc.revoked {
				t.Fatal("grant revocation misclassified")
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatal("transport cause lost")
			}
			if strings.Contains(err.Error(), "private-token") {
				t.Fatal("secret in error")
			}
		})
	}
}

func TestLogoutDeletesCorruptSessionAndReportsRevokeFailure(t *testing.T) {
	var revocations atomic.Int32
	app, store, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		revocations.Add(1)
		return behaviorResponse(503, ""), nil
	})
	sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Logout(context.Background(), data.AccountDID, data.SessionID); err == nil {
		t.Fatal("failed revoke reported success")
	}
	if revocations.Load() != 2 {
		t.Fatal("did not attempt both revocations")
	}
	if _, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID); err == nil {
		t.Fatal("logout session resumed")
	}
	if _, err := sess.RefreshTokens(context.Background()); !errors.Is(err, ErrGrantRevoked) {
		t.Fatal("outstanding session holder refreshed after logout")
	}
	if err := sess.UpdateHostDPoPNonce(context.Background(), "late-nonce"); !errors.Is(err, ErrGrantRevoked) {
		t.Fatal("late nonce persisted after logout")
	}
	if _, err := store.GetSession(context.Background(), data.AccountDID, data.SessionID); err == nil {
		t.Fatal("late session holder resurrected deleted session")
	}
	data.SessionID = "corrupt"
	data.DPoPPrivateKeyMultibase = "not a key"
	store.SaveSession(context.Background(), data)
	if err := app.Logout(context.Background(), data.AccountDID, data.SessionID); err == nil {
		t.Fatal("corrupt session error was lost")
	}
	if _, err := store.GetSession(context.Background(), data.AccountDID, data.SessionID); err == nil {
		t.Fatal("unresumable session not deleted")
	}
}

func TestStartAuthFlowDoesNotRedirectWhenStateCannotBeSaved(t *testing.T) {
	metadata, err := os.ReadFile("testdata/bsky-entryway-authorization-server.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := behaviorTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return behaviorResponse(200, string(metadata)), nil
		}
		return behaviorResponse(201, `{"request_uri":"urn:ietf:params:oauth:request_uri:example","expires_in":60}`), nil
	})
	app, store, _ := behaviorApp(t, transport)
	app.Resolver.Client = app.Client
	store.requestErr = errors.New("state store unavailable")
	redirect, err := app.StartAuthFlow(context.Background(), "https://bsky.social")
	if redirect != "" || !errors.Is(err, store.requestErr) {
		t.Fatalf("redirect=%q, err=%v", redirect, err)
	}
}

func TestRefreshOmittedScopeRetainsExistingConsent(t *testing.T) {
	app, store, data := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		return behaviorResponse(200, `{"access_token":"new","refresh_token":"rotated"}`), nil
	})
	sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.RefreshTokens(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetSession(context.Background(), data.AccountDID, data.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(saved.Scopes, " ") != strings.Join(data.Scopes, " ") {
		t.Fatal("omitted scope erased consent")
	}
}

func TestResumedSessionIsolationAcrossAccountsAndSessionIDs(t *testing.T) {
	app, store, original := behaviorApp(t, func(r *http.Request) (*http.Response, error) {
		return behaviorResponse(200, r.Header.Get("Authorization")), nil
	})
	otherSession := original
	otherSession.SessionID = "second-session"
	otherSession.AccessToken = "second-token"
	otherAccount := original
	otherAccount.AccountDID = "did:plc:other"
	otherAccount.AccessToken = "other-account-token"
	for _, data := range []ClientSessionData{original, otherSession, otherAccount} {
		if err := store.SaveSession(context.Background(), data); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []ClientSessionData{otherSession, original, otherAccount, original} {
		sess, err := app.ResumeSession(context.Background(), data.AccountDID, data.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest("GET", data.HostURL+"/resource", nil)
		resp, err := sess.DoWithAuth(app.Client, req, "com.example.read")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "DPoP "+data.AccessToken {
			t.Fatal("session cache crossed account/session boundary")
		}
	}
}
