package oauth

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type OAuthStore interface {
	GetSession(ctx context.Context, did syntax.DID) (*SessionData, error)
	SaveSession(ctx context.Context, sess SessionData) error
	DeleteSession(ctx context.Context, did syntax.DID) error
	GetAuthRequestInfo(ctx context.Context, state string) (*AuthRequestData, error)
	SaveAuthRequestInfo(ctx context.Context, info AuthRequestData) error
	DeleteAuthRequestInfo(ctx context.Context, state string) error
}
