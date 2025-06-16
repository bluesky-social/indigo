package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/gorilla/sessions"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:   "atp-oauth-demo",
		Usage:  "demo OAuth web server",
		Action: runServer,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "session-secret",
				Usage:    "random string/token used for session cookie security",
				Required: true,
				EnvVars:  []string{"SESSION_SECRET"},
			},
			&cli.StringFlag{
				Name:     "client-secret-key",
				Usage:    "confidential client secret key. should be P-256 private key in multibase encoding",
				Required: true,
				EnvVars:  []string{"CLIENT_SECRET_KEY"},
			},
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

type Server struct {
	CookieStore *sessions.CookieStore
	AuthStore   AuthMemStore
	Dir         identity.Directory
	Config      oauth.ClientConfig
	Resolver    *oauth.Resolver
}

//go:embed "base.html"
var tmplBaseText string

//go:embed "home.html"
var tmplHomeText string
var tmplHome = template.Must(template.Must(template.New("home.html").Parse(tmplBaseText)).Parse(tmplHomeText))

//go:embed "login.html"
var tmplLoginText string
var tmplLogin = template.Must(template.Must(template.New("login.html").Parse(tmplBaseText)).Parse(tmplLoginText))

//go:embed "post.html"
var tmplPostText string
var tmplPost = template.Must(template.Must(template.New("post.html").Parse(tmplBaseText)).Parse(tmplPostText))

func (s *Server) Homepage(w http.ResponseWriter, r *http.Request) {
	tmplHome.Execute(w, nil)
}

func runServer(cctx *cli.Context) error {

	priv, err := crypto.ParsePrivateMultibase(cctx.String("client-secret-key"))
	if err != nil {
		return err
	}
	// NOTE: initializing with empty callback and client ID; will initialize from request hostname
	conf := oauth.NewClientConfig("", "")
	conf.PrivateKey = priv
	srv := Server{
		CookieStore: sessions.NewCookieStore([]byte(cctx.String("session-secret"))),
		AuthStore:   NewAuthMemStore(),
		Dir:         identity.DefaultDirectory(),
		Config:      conf,
		Resolver:    oauth.NewResolver(),
	}

	http.HandleFunc("GET /", srv.Homepage)
	http.HandleFunc("GET /oauth/client-metadata.json", srv.ClientMetadata)
	http.HandleFunc("GET /oauth/jwks.json", srv.JWKS)
	http.HandleFunc("GET /oauth/login", srv.OAuthLogin)
	http.HandleFunc("POST /oauth/login", srv.OAuthLogin)
	http.HandleFunc("GET /oauth/callback", srv.OAuthCallback)
	http.HandleFunc("GET /oauth/refresh", srv.OAuthRefresh)
	http.HandleFunc("GET /oauth/logout", srv.OAuthLogout)
	http.HandleFunc("GET /bsky/post", srv.Post)
	http.HandleFunc("POST /bsky/post", srv.Post)

	bind := ":8080"
	slog.Info("starting http server", "bind", bind)
	if err := http.ListenAndServe(bind, nil); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	return nil
}

func strPtr(raw string) *string {
	return &raw
}

func (s *Server) finishConfig(r *http.Request) {
	host := r.Host
	if host == "" {
		return
	}
	if s.Config.ClientID == "" {
		s.Config.ClientID = fmt.Sprintf("https://%s/oauth/client-metadata.json", host)
		s.Config.CallbackURL = fmt.Sprintf("https://%s/oauth/callback", host)
	}
	return
}

func (s *Server) ClientMetadata(w http.ResponseWriter, r *http.Request) {
	slog.Info("client metadata request", "url", r.URL, "host", r.Host)
	s.finishConfig(r)

	scope := "atproto transition:generic"
	meta := s.Config.ClientMetadata(scope)
	meta.JWKSUri = strPtr(fmt.Sprintf("https://%s/oauth/jwks.json", r.Host))
	meta.ClientName = strPtr("indigo atp-oauth-demo")
	meta.ClientURI = strPtr(fmt.Sprintf("https://%s", r.Host))

	// internal consistency check
	if err := meta.Validate(s.Config.ClientID); err != nil {
		slog.Error("validating client metadata", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

func (s *Server) JWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	body := s.Config.PublicJWKS()
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) OAuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.finishConfig(r)

	if r.Method != "POST" {
		tmplLogin.Execute(w, nil)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Errorf("parsing form data: %w", err).Error(), http.StatusBadRequest)
		return
	}

	// TODO: auth server URL support
	username := r.PostFormValue("username")
	atid, err := syntax.ParseAtIdentifier(username)
	if err != nil {
		http.Error(w, fmt.Errorf("not a valid account identifier (%s): %w", username, err).Error(), http.StatusBadRequest)
		return
	}
	ident, err := s.Dir.Lookup(ctx, *atid)
	if err != nil {
		http.Error(w, fmt.Errorf("failed to resolve username (%s): %w", username, err).Error(), http.StatusBadRequest)
		return
	}
	host := ident.PDSEndpoint()
	if host == "" {
		http.Error(w, "identity does not link to an atproto host (PDS)", http.StatusBadRequest)
		return
	}

	logger := slog.Default().With("did", ident.DID, "handle", ident.Handle, "host", host)
	logger.Info("resolving to auth server metadata")
	authserverURL, err := s.Resolver.ResolveAuthServerURL(ctx, host)
	if err != nil {
		http.Error(w, fmt.Errorf("resolving auth server: %w", err).Error(), http.StatusBadRequest)
		return
	}
	authserverMeta, err := s.Resolver.ResolveAuthServerMetadata(ctx, authserverURL)
	if err != nil {
		http.Error(w, fmt.Errorf("fetching auth server metadata: %w", err).Error(), http.StatusBadRequest)
		return
	}

	callbackURL, err := url.Parse(s.Config.ClientID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	callbackURL.Path = "/oauth/callback"
	s.Config.CallbackURL = callbackURL.String()

	scope := "atproto transition:generic"
	info, err := s.Config.SendAuthRequest(ctx, authserverMeta, username, scope)
	if err != nil {
		http.Error(w, fmt.Errorf("auth request failed: %w", err).Error(), http.StatusBadRequest)
		return
	}

	// XXX:
	info.AccountDID = &ident.DID

	// persist auth request info
	s.AuthStore.SaveAuthRequestInfo(*info)

	params := url.Values{}
	params.Set("client_id", s.Config.ClientID)
	params.Set("request_uri", info.RequestURI)
	// TODO: check that 'authorization_endpoint' is "safe" (?)
	redirectURL := fmt.Sprintf("%s?%s", authserverMeta.AuthorizationEndpoint, params.Encode())
	http.Redirect(w, r, redirectURL, http.StatusFound)
	return
}

func (s *Server) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	params := r.URL.Query()
	slog.Info("received callback", "params", params)
	state := params.Get("state")
	authserverURL := params.Get("iss")
	authCode := params.Get("code")
	if state == "" || authserverURL == "" || authCode == "" {
		http.Error(w, "missing required query param", http.StatusBadRequest)
		return
	}

	info, err := s.AuthStore.GetAuthRequestInfo(state)
	if err != nil {
		http.Error(w, fmt.Errorf("loading auth request info: %w", err).Error(), http.StatusNotFound)
		return
	}

	if info.State != state || info.AuthServerURL != authserverURL {
		http.Error(w, "callback params don't match request info", http.StatusBadRequest)
		return
	}

	tokenResp, err := s.Config.SendInitialTokenRequest(ctx, authCode, *info)
	if err != nil {
		http.Error(w, fmt.Errorf("initial token request: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// XXX: verify against initial request info (DID, handle, etc)
	// - account identifier (if started with that)
	// - if started with PDS URL, resolve identity, and then resolve PDS to auth server, and check it all matches
	if info.AccountDID == nil || tokenResp.Subject != info.AccountDID.String() {
		http.Error(w, "token subject didn't match original DID", http.StatusBadRequest)
		return
	}

	// TODO: could be flexible instead of considering this a hard failure?
	if tokenResp.Scope != info.Scope {
		http.Error(w, "token scope didn't match original request", http.StatusBadRequest)
		return
	}

	authSess := oauth.SessionData{
		AccountDID:          *info.AccountDID,   // nil checked above
		HostURL:             info.AuthServerURL, // XXX
		AuthServerURL:       info.AuthServerURL,
		AccessToken:         tokenResp.AccessToken,
		RefreshToken:        tokenResp.RefreshToken,
		DpopAuthServerNonce: info.DpopAuthServerNonce,
		DpopHostNonce:       info.DpopAuthServerNonce, // XXX
		DpopKeyMultibase:    info.DpopKeyMultibase,
	}
	s.AuthStore.SaveSession(authSess)

	// create signed cookie session, indicating account DID
	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	sess.Values["account_did"] = authSess.AccountDID.String()
	if err := sess.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("login successful", "did", authSess.AccountDID.String())
	http.Redirect(w, r, "/bsky/post", http.StatusFound)
}

func (s *Server) OAuthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	accountDID, ok := sess.Values["account_did"].(string)
	if !ok || accountDID == "" {
		// TODO: suppowed to set a WWW header; and could redirect?
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	sessData, err := s.AuthStore.GetSession(did)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	oauthSess, err := oauth.ResumeSession(&s.Config, sessData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := oauthSess.RefreshTokens(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.AuthStore.SaveSession(*oauthSess.Data)
	slog.Info("refreshed tokens")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) OAuthLogout(w http.ResponseWriter, r *http.Request) {
	// XXX: delete session from auth store

	// wipe all secure cookie session data
	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	sess.Values = make(map[any]any)
	err := sess.Save(r, w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("logged out")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) Post(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slog.Info("in post handler")

	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	accountDID, ok := sess.Values["account_did"].(string)
	if !ok || accountDID == "" {
		// TODO: suppowed to set a WWW header; and could redirect?
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	sessData, err := s.AuthStore.GetSession(did)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.Method != "POST" {
		tmplPost.Execute(w, nil)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Errorf("parsing form data: %w", err).Error(), http.StatusBadRequest)
		return
	}
	text := r.PostFormValue("post_text")

	oauthSess, err := oauth.ResumeSession(&s.Config, sessData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c := client.NewAPIClient(oauthSess.Data.HostURL)
	c.Auth = oauthSess
	c.Headers.Set("User-Agent", "indigo-oauth-demo")

	body := map[string]any{
		"repo":       oauthSess.Data.AccountDID.String(),
		"collection": "app.bsky.feed.post",
		"record": map[string]any{
			"$type":     "app.bsky.feed.post",
			"text":      text,
			"createdAt": syntax.DatetimeNow(),
		},
	}

	slog.Info("attempting post...", "text", text)
	if err := c.Post(ctx, "com.atproto.repo.createRecord", body, nil); err != nil {
		http.Error(w, fmt.Errorf("posting failed: %w", err).Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/bsky/post", http.StatusFound)
}
