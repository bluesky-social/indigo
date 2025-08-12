/*
OAuth implementation for atproto, currently focused on clients.

Feature set includes:

  - client and server metadata resolution
  - PKCE: computing and verifying challenges
  - DPoP client implementation: JWT signing and nonces for requests to Auth Server and Resource Server
  - PAR client submission
  - both public and confidential clients, with support for signed client attestations in the later case

Most OAuth client applications will use the high-level [ClientApp] and supporting interfaces to manage session logins, persistence, and token refreshes. Lower-level components are designed to be used in isolation if needed.

This package does not contain supporting code for atproto permissions or permission sets. It treats scopes as simple strings.

# Quickstart

Create a single [ClientApp] instance during service setup that will be used (concurrently) across all users and sessions:

	config := oauth.NewPublicConfig(
		"https://app.example.com/client-metadata.json",
		"https://app.example.com/oauth/callback",
		[]string{"atproto", "transition:generic"},
	)

	// clients are "public" by default, but if they have secure access to a secret attestation key can be "confidential"
	if CLIENT_SECRET_KEY != "" {
		priv, err := crypto.ParsePrivateMultibase(CLIENT_SECRET_KEY)
		if err != nil {
			return err
		}
		if err := config.SetClientSecret(priv, "example1"); err != nil {
			return err
		}
	}

	oauthApp := oauth.NewClientApp(&config, oauth.NewMemStore())

For a real service, you would want to use a database or other peristant implementation of the [ClientAuthStore] interface instead of [MemStore]. Otherwise all user sessions are dropped every time the process restarts.

The client metadata document needs to be served at the URL indicated by the 'client_id'. This can be done statically, or dynamically generated and served from the configuration:

	http.HandleFunc("GET /client-metadata.json", HandleClientMetadata)

	func HandleClientMetadata(w http.ResponseWriter, r *http.Request) {
		doc := oauthApp.Config.ClientMetadata()

		// if this is is a confidential client, need to set doc.JWKSURI, and implement a handler

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

The login auth flow starts with a user identifier, which could be an atproto handle, DID, or an auth server URL (eg, a PDS). The high-level [ClientApp.StartAuthFlow] method will resolve the identifier, send an auth request (PAR) to the server, persist request metadata in the [ClientAuthStore], and return a redirect URL for the user to visit (usually the PDS):

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

The service then waits for a callback request on the configured endpoint. The [ClientApp.ProcessCallback] method will load the earlier request metadata from the [ClientAuthStore], send an initial token request to the auth server, and validate that the session is consistent with the identifier from the beginning of the login flow.

	http.HandleFunc("GET /oauth/callback", HandleOAuthCallback)

	func HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		sessData, err := oauthApp.ProcessCallback(ctx, r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		// web services might record the DID and session ID in a secure session cookie
		_ = sessData.AccountDID
		_ = sessData.SessionID

		// the returned scopes might not include all of those requested
		_ = sessData.Scopes

		http.Redirect(w, r, "/app", http.StatusFound)
	}

Sessions can be resumed and used to make authenticated API calls to the user's host:

	// web services might use a secure session cookie to determine user's DID for a request
	did := syntax.DID("did:plc:abc123")
	sessionID := "xyz"

	sess, err := oauthApp.ResumeSession(ctx, did, sessionID)
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

The [ClientSession] will handle nonce updates and token refreshes, and persist the results in the [ClientAuthStore].

To log out a user, delete their session from the [ClientAuthStore]:

	if err := oauthApp.Store.DeleteSession(r.Context(), did, sessionID); err != nil {
		return err
	}

# Authorization-only Situations

Some applications might only use atproto OAuth for authorization (authn). For example, "Login with Atmospehre", where the application does not need to access additional account metadata (such as account email), or access any restricted account resources (eg, write to atproto repository).

In this scenario, the client app still needs to do an initial token request, to confirm the account identifier. But the returned session tokens will never be used, and do not need to be persisted.

In these scenarios, applications could use an implementation of [ClientAuthStore] which does not actually persist the session data when [ClientAuthStore.SaveSession] is called. Or, the application could immediately call [ClientAuthStore.DeleteSession] after [ClientApp.ProcessCallback] returns.

# Multiple Sessions Per Account

In the traditional web app backend scenario, a single account (DID) might have multiple active sessions. For example, a user might log in from a browser on their laptop and on a mobile device at the same time. The user must go through the entire flow on each device (or browser) to authenticate the user. To prevent a new session from "clobbering" existing sessions (including tokens), this package supports multiple concurrent sessions per account, distinguished by a session ID. The random 'state' token from the auth flow is re-used by default.

In other scenarious, multiple sessions are not needed or desirable. For example, an integration backend, or tool with very short session lifetimes. In these scenarios, implementations of the [ClientAuthStore] interface could ignore the session ID. Or the [ClientApp] could be configured with an ephemeral [ClientAuthStore] (to support auth flows), and managed the session data returned by [ClientApp.ProcessCallback] using separate session storage logic.
*/
package oauth
