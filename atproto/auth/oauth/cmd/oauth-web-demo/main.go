package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/gorilla/sessions"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:   "oauth-web-demo",
		Usage:  "atproto OAuth web server demo",
		Action: runServer,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "session-secret",
				Usage:    "random string/token used for session cookie security",
				Required: true,
				EnvVars:  []string{"SESSION_SECRET"},
			},
			&cli.StringFlag{
				Name:    "hostname",
				Usage:   "public host name for this client (if not localhost dev mode)",
				EnvVars: []string{"CLIENT_HOSTNAME"},
			},
			&cli.StringFlag{
				Name:    "client-secret-key",
				Usage:   "confidential client secret key. should be P-256 private key in multibase encoding",
				EnvVars: []string{"CLIENT_SECRET_KEY"},
			},
			&cli.StringFlag{
				Name:    "client-secret-key-id",
				Usage:   "key id for client-secret-key",
				Value:   "primary",
				EnvVars: []string{"CLIENT_SECRET_KEY_ID"},
			},
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

type Server struct {
	CookieStore *sessions.CookieStore
	Dir         identity.Directory
	OAuth       *oauth.ClientApp
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

func runServer(cctx *cli.Context) error {

	scopes := []string{"atproto", "transition:generic"}
	bind := ":8080"

	var config oauth.ClientConfig
	hostname := cctx.String("hostname")
	if hostname == "" {
		config = oauth.NewLocalhostConfig(
			fmt.Sprintf("http://127.0.0.1%s/oauth/callback", bind),
			scopes,
		)
		slog.Info("configuring localhost OAuth client", "CallbackURL", config.CallbackURL)
	} else {
		config = oauth.NewPublicConfig(
			fmt.Sprintf("https://%s/oauth/client-metadata.json", hostname),
			fmt.Sprintf("https://%s/oauth/callback", hostname),
			scopes,
		)
	}

	// If a client secret key is provided (as a multibase string), turn this in to a confidential client
	if cctx.String("client-secret-key") != "" && hostname != "" {
		priv, err := crypto.ParsePrivateMultibase(cctx.String("client-secret-key"))
		if err != nil {
			return err
		}
		if err := config.SetClientSecret(priv, cctx.String("client-secret-key-id")); err != nil {
			return err
		}
		slog.Info("configuring confidential OAuth client")
	}

	oauthClient := oauth.NewClientApp(&config, oauth.NewMemStore())

	srv := Server{
		CookieStore: sessions.NewCookieStore([]byte(cctx.String("session-secret"))),
		Dir:         identity.DefaultDirectory(),
		OAuth:       oauthClient,
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

	slog.Info("starting http server", "bind", bind)
	if err := http.ListenAndServe(bind, nil); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	return nil
}

func (s *Server) currentSessionDID(r *http.Request) (*syntax.DID, string) {
	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	accountDID, ok := sess.Values["account_did"].(string)
	if !ok || accountDID == "" {
		return nil, ""
	}
	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		return nil, ""
	}
	sessionID, ok := sess.Values["session_id"].(string)
	if !ok || sessionID == "" {
		return nil, ""
	}

	return &did, sessionID
}

func strPtr(raw string) *string {
	return &raw
}

func (s *Server) ClientMetadata(w http.ResponseWriter, r *http.Request) {
	slog.Info("client metadata request", "url", r.URL, "host", r.Host)

	meta := s.OAuth.Config.ClientMetadata()
	if s.OAuth.Config.IsConfidential() {
		meta.JWKSURI = strPtr(fmt.Sprintf("https://%s/oauth/jwks.json", r.Host))
	}
	meta.ClientName = strPtr("indigo atp-oauth-demo")
	meta.ClientURI = strPtr(fmt.Sprintf("https://%s", r.Host))

	// internal consistency check
	if err := meta.Validate(s.OAuth.Config.ClientID); err != nil {
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
	body := s.OAuth.Config.PublicJWKS()
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) Homepage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// attempts to load Session to display links
	did, sessionID := s.currentSessionDID(r)
	if did == nil {
		tmplHome.Execute(w, nil)
		return
	}

	_, err := s.OAuth.ResumeSession(ctx, *did, sessionID)
	if err != nil {
		tmplHome.Execute(w, nil)
		return
	}
	tmplHome.Execute(w, did)
}

func (s *Server) OAuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != "POST" {
		tmplLogin.Execute(w, nil)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Errorf("parsing form data: %w", err).Error(), http.StatusBadRequest)
		return
	}

	username := r.PostFormValue("username")

	slog.Info("OAuthLogin", "client_id", s.OAuth.Config.ClientID, "callback_url", s.OAuth.Config.CallbackURL)

	redirectURL, err := s.OAuth.StartAuthFlow(ctx, username)
	if err != nil {
		http.Error(w, fmt.Errorf("OAuth login failed: %w", err).Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
	return
}

func (s *Server) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	params := r.URL.Query()
	slog.Info("received callback", "params", params)

	sessData, err := s.OAuth.ProcessCallback(ctx, r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Errorf("processing OAuth callback: %w", err).Error(), http.StatusBadRequest)
		return
	}

	// create signed cookie session, indicating account DID
	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	sess.Values["account_did"] = sessData.AccountDID.String()
	sess.Values["session_id"] = sessData.SessionID
	if err := sess.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("login successful", "did", sessData.AccountDID.String())
	http.Redirect(w, r, "/bsky/post", http.StatusFound)
}

func (s *Server) OAuthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	did, sessionID := s.currentSessionDID(r)
	if did == nil {
		// TODO: supposed to set a WWW header; and could redirect?
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	oauthSess, err := s.OAuth.ResumeSession(ctx, *did, sessionID)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	_, err = oauthSess.RefreshTokens(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.OAuth.Store.SaveSession(ctx, *oauthSess.Data)
	slog.Info("refreshed tokens")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) OAuthLogout(w http.ResponseWriter, r *http.Request) {

	// delete session from auth store
	did, sessionID := s.currentSessionDID(r)
	if did != nil {
		if err := s.OAuth.Store.DeleteSession(r.Context(), *did, sessionID); err != nil {
			slog.Error("failed to delete session", "did", did, "err", err)
		}
	}

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

	did, sessionID := s.currentSessionDID(r)
	if did == nil {
		// TODO: supposed to set a WWW header; and could redirect?
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		tmplPost.Execute(w, did)
		return
	}

	oauthSess, err := s.OAuth.ResumeSession(ctx, *did, sessionID)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	c := oauthSess.APIClient()

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Errorf("parsing form data: %w", err).Error(), http.StatusBadRequest)
		return
	}
	text := r.PostFormValue("post_text")

	body := map[string]any{
		"repo":       c.AccountDID.String(),
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
