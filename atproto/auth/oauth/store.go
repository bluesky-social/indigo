package oauth

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Interface for persisting session data and auth request data, required as part of an OAuth client app.
//
// Implementations should allow for concurrent access.
type ClientAuthStore interface {
	GetSession(ctx context.Context, did syntax.DID) (*ClientSessionData, error)
	SaveSession(ctx context.Context, sess ClientSessionData) error
	DeleteSession(ctx context.Context, did syntax.DID) error

	GetAuthRequestInfo(ctx context.Context, state string) (*AuthRequestData, error)
	SaveAuthRequestInfo(ctx context.Context, info AuthRequestData) error
	DeleteAuthRequestInfo(ctx context.Context, state string) error
}
