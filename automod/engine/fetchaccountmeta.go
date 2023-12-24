package engine

import (
	"context"
	"encoding/json"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/util"
)

func (e *Engine) GetAccountMeta(ctx context.Context, ident *identity.Identity) (*AccountMeta, error) {

	// wipe parsed public key; it's a waste of space and can't serialize
	ident.ParsedPublicKey = nil

	// fallback in case client wasn't configured (eg, testing)
	if e.BskyClient == nil {
		e.Logger.Warn("skipping account meta hydration")
		am := AccountMeta{
			Identity: ident,
			Profile:  ProfileSummary{},
		}
		return &am, nil
	}

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
	var negLabels []string
	for _, lbl := range pv.Labels {
		if lbl.Neg != nil && *lbl.Neg == true {
			negLabels = append(negLabels, lbl.Val)
		} else {
			labels = append(labels, lbl.Val)
		}
	}

	flags, err := e.Flags.Get(ctx, ident.DID.String())
	if err != nil {
		return nil, err
	}

	am := AccountMeta{
		Identity: ident,
		Profile: ProfileSummary{
			HasAvatar:   pv.Avatar != nil,
			Description: pv.Description,
			DisplayName: pv.DisplayName,
		},
		AccountLabels:        util.DedupeStrings(labels),
		AccountNegatedLabels: util.DedupeStrings(negLabels),
		AccountFlags:         flags,
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
		pv, err := comatproto.AdminGetAccountInfo(ctx, e.AdminClient, ident.DID.String())
		if err != nil {
			return nil, err
		}
		ap := AccountPrivate{}
		if pv.Email != nil && *pv.Email != "" {
			ap.Email = *pv.Email
		}
		if pv.EmailConfirmedAt != nil && *pv.EmailConfirmedAt != "" {
			ap.EmailConfirmed = true
		}
		ts, err := syntax.ParseDatetimeTime(pv.IndexedAt)
		if err != nil {
			return nil, err
		}
		ap.IndexedAt = ts
		am.Private = &ap
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
