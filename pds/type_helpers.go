package pds

import (
	"context"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

// TODO: this is a quick helper to transform bsky.ActorDefs_ProfileViewBasic to
// bsky.ActorDefs_ProfileView, written during the lexicon refactor (March
// 2023). It probably needs to be updated to actually populate all the
// additional fields (eg, via additional database queries)
func (s *Server) actorBasicToView(ctx context.Context, basic *appbsky.ActorDefs_ProfileViewBasic) *appbsky.ActorDefs_ProfileView {
	view := &appbsky.ActorDefs_ProfileView{
		Avatar:      basic.Avatar,
		Did:         basic.Did,
		DisplayName: basic.DisplayName,
		Handle:      basic.Handle,
		Viewer:      basic.Viewer,
	}
	return view
}
