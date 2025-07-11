package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
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

func (s *Server) Homepage(w http.ResponseWriter, r *http.Request) {
	tmplHome.Execute(w, nil)
}

func runServer(cctx *cli.Context) error {

	// TODO: localhost dev mode if hostname is empty
	hostname := cctx.String("hostname")
	conf := oauth.NewPublicConfig(
		fmt.Sprintf("https://%s/oauth/client-metadata.json", hostname),
		fmt.Sprintf("https://%s/oauth/callback", hostname),
	)

	// If a client secret key is provided (as a multibase string), turn this in to a confidential client
	if cctx.String("client-secret-key") != "" {
		priv, err := crypto.ParsePrivateMultibase(cctx.String("client-secret-key"))
		if err != nil {
			return err
		}
		conf.AddClientSecret(priv, cctx.String("client-secret-key-id"))
	}

	oauthClient := oauth.NewClientApp(&conf, oauth.NewMemStore())

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

	bind := ":8080"
	slog.Info("starting http server", "bind", bind)
	if err := http.ListenAndServe(bind, nil); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	return nil
}

func (s *Server) loadOAuthSession(r *http.Request) (*oauth.ClientSession, error) {
	ctx := r.Context()

	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	accountDID, ok := sess.Values["account_did"].(string)
	if !ok || accountDID == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		return nil, err
	}

	return s.OAuth.ResumeSession(ctx, did)
}

func strPtr(raw string) *string {
	return &raw
}

func (s *Server) ClientMetadata(w http.ResponseWriter, r *http.Request) {
	slog.Info("client metadata request", "url", r.URL, "host", r.Host)

	scope := "atproto transition:generic"
	meta := s.OAuth.Config.ClientMetadata(scope)
	meta.JWKSUri = strPtr(fmt.Sprintf("https://%s/oauth/jwks.json", r.Host))
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
	}

	// create signed cookie session, indicating account DID
	sess, _ := s.CookieStore.Get(r, "oauth-demo")
	sess.Values["account_did"] = sessData.AccountDID.String()
	if err := sess.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("login successful", "did", sessData.AccountDID.String())
	http.Redirect(w, r, "/bsky/post", http.StatusFound)
}

func (s *Server) OAuthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	oauthSess, err := s.loadOAuthSession(r)
	if err != nil {
		// TODO: suppowed to set a WWW header; and could redirect?
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	if err := oauthSess.RefreshTokens(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.OAuth.Store.SaveSession(ctx, *oauthSess.Data)
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

	if r.Method != "POST" {
		tmplPost.Execute(w, nil)
		return
	}

	oauthSess, err := s.loadOAuthSession(r)
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
