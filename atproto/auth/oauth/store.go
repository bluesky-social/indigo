package oauth

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Interface for persisting session data and auth request data, required as part of an OAuth client app.
//
// This interface supports multiple sessions for a single account (DID). This is helpful for traditional web app backends where a single user might log in and have concurrent sessions from multiple browsers/devices. For situations where multiple sessions are not required, implementations of this interface could ignore the `sessionID` parameters, though this could result in clobbering of active sessions.
//
// For authorization-only (authn-only) applications, the `SaveSession()` method could be a no-op.
//
// Implementations should generally allow for concurrent access.
type ClientAuthStore interface {
	GetSession(ctx context.Context, did syntax.DID, sessionID string) (*ClientSessionData, error)
	SaveSession(ctx context.Context, sess ClientSessionData) error
	DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error

	GetAuthRequestInfo(ctx context.Context, state string) (*AuthRequestData, error)
	SaveAuthRequestInfo(ctx context.Context, info AuthRequestData) error
	DeleteAuthRequestInfo(ctx context.Context, state string) error
}
