/*
OAuth implementation for atproto, currently focused on clients.

Feature set includes:

- client and server metadata resolution
- PKCE: computing and verifying challenges
- DPoP client implementation: JWT signing and nonces for requests to Auth Server and Resource Server
- PAR client submission
- both public and confidential clients, with support for signed client attestations in the later case

Most OAuth client applications will use the high-level [ClientApp] and supporting interfaces to manage session logins, persistance, and token refreshes. Lower-level components are designed to be used in isolation if needed.

This package does not contain supporting code for atproto permissions or permission sets. It treats scopes as simple strings.

## Quickstart

Create a single [ClientApp] instance during service setup that will be used (concurrently) across all users and sessions:

```
oauthScope := "atproto transition:generic"
config := oauth.NewPublicConfig(
	"https://app.example.com/client-metadata.json",
	"https://app.example.com/oauth/callback",
)

// clients are "public" by default, but if they have secure access to a secret attestation key can be "confidential"

if CLIENT_SECRET_KEY != "" {
	priv, err := crypto.ParsePrivateMultibase(CLIENT_SECRET_KEY)
	if err != nil {
	    return err
	}
	if err := config.AddClientSecret(priv, "example1"); err != nil {
	    return err
	}
}

oauthApp := oauth.NewClientApp(&config, oauth.NewMemStore())
```

For a real service, you would want to use a database or other peristant storage instead of [MemStore]. Otherwise all user sessions are dropped every time the process restarts.

The client metadata document needs to be served at the URL indicated by the `client_id`. This can be done statically, or dynamically generated and served from the configuation:

```
http.HandleFunc("GET /client-metadata.json", HandleClientMetadata)

func HandleClientMetadata(w http.ResponseWriter, r *http.Request) {
	doc := oauthApp.Config.ClientMetadata(oauthScope)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
```

The login auth flow starts with a user identifier, which could be an atproto handle, DID, or a host URL. The high-level [StartAuthFlow()] method will resolve the identifier, send an auth request (PAR) to the server, persist request metadata in the [OAuthStore], and return a redirect URL for the user to visit:

```
http.HandleFunc("GET /oauth/login", HandleLogin)

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// parse login identifier from the request
	identifier := "..."

	redirectURL, err := oauthApp.StartAuthFlow(ctx, identifier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
```

The service then waits for a callback request on the configured endpoint. The [ProcessCallback()] method will load the earlier request metadata from the [OAuthStore], send an initial token request to the auth server, and validate that the session is consistent with the identifier from the begining of the login flow.

```
http.HandleFunc("GET /client-metadata.json", HandleClientMetadata)

func HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessData, err := oauthApp.ProcessCallback(ctx, r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	// web services might record the DID in a secure session cookie
	_ = sessData.AccountDID

	http.Redirect(w, r, "/app", http.StatusFound)
}
```

Finally, sessions can be resumed and used to make authenticated API calls to the user's host:

```
// web services might use a secure session cookie to determine user's DID for a request
did := syntax.DID("did:plc:abc123")

sess, err := oauthApp.ResumeSession(ctx, did)
if err != nil {
	return err
}

c := sess.APIClient()

body := map[string]any{
	"repo":       *c.AccountDID,
	"collection": "app.bsky.feed.post",
	"record": map[string]any{
	    "$type":     "app.bsky.feed.post",
	    "text":      "Hello World via OAuth!",
	    "createdAt": syntax.DatetimeNow(),
	},
}

if err := c.Post(ctx, "com.atproto.repo.createRecord", body, nil); err != nil {
	return err
}
```

The [ClientSession] will handle nonce updates and token refreshes, and persist the results in the [OAuthStore].

TODO: logout
*/
package oauth
