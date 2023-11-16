package automod

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
)

type ProfileSummary struct {
	HasAvatar   bool
	Description *string
	DisplayName *string
}

type AccountPrivate struct {
	Email          string
	EmailConfirmed bool
}

// information about a repo/account/identity, always pre-populated and relevant to many rules
type AccountMeta struct {
	Identity       *identity.Identity
	Profile        ProfileSummary
	Private        *AccountPrivate
	AccountLabels  []string
	FollowersCount int64
	FollowsCount   int64
	PostsCount     int64
	IndexedAt      *time.Time
}

func (e *Engine) GetAccountMeta(ctx context.Context, ident *identity.Identity) (*AccountMeta, error) {

	// wipe parsed public key; it's a waste of space and can't serialize
	ident.ParsedPublicKey = nil

	existing, err := e.Cache.Get(ctx, "acct", ident.DID.String())
	if err != nil {
		return nil, err
	}
	if existing != "" {
		var am AccountMeta
		err := json.Unmarshal([]byte(existing), &am)
		if err != nil {
			return nil, fmt.Errorf("parsing AccountMeta from cache: %v", err)
		}
		am.Identity = ident
		return &am, nil
	}

	// fetch account metadata
	pv, err := appbsky.ActorGetProfile(ctx, e.BskyClient, ident.DID.String())
	if err != nil {
		return nil, err
	}

	var labels []string
	for _, lbl := range pv.Labels {
		labels = append(labels, lbl.Val)
	}

	am := AccountMeta{
		Identity: ident,
		Profile: ProfileSummary{
			HasAvatar:   pv.Avatar != nil,
			Description: pv.Description,
			DisplayName: pv.DisplayName,
		},
		AccountLabels: dedupeStrings(labels),
	}
	if pv.PostsCount != nil {
		am.PostsCount = *pv.PostsCount
	}
	if pv.FollowersCount != nil {
		am.FollowersCount = *pv.FollowersCount
	}
	if pv.FollowsCount != nil {
		am.FollowsCount = *pv.FollowsCount
	}

	if e.AdminClient != nil {
		// XXX: get admin-level info (email, indexed at, etc). requires lexgen update
	}

	val, err := json.Marshal(&am)
	if err != nil {
		return nil, err
	}

	if err := e.Cache.Set(ctx, "acct", ident.DID.String(), string(val)); err != nil {
		return nil, err
	}
	return &am, nil
}
