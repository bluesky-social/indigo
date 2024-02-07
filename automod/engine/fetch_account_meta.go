package engine

import (
	"context"
	"encoding/json"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Helper to hydrate metadata about an account from several sources: PDS (if access), mod service (if access), public identity resolution
func (e *Engine) GetAccountMeta(ctx context.Context, ident *identity.Identity) (*AccountMeta, error) {

	logger := e.Logger.With("did", ident.DID.String())

	// wipe parsed public key; it's a waste of space and can't serialize
	ident.ParsedPublicKey = nil

	// fallback in case client wasn't configured (eg, testing)
	if e.BskyClient == nil {
		logger.Warn("skipping account meta hydration")
		am := AccountMeta{
			Identity: ident,
			Profile:  ProfileSummary{},
		}
		return &am, nil
	}

	existing, err := e.Cache.Get(ctx, "acct", ident.DID.String())
	if err != nil {
		return nil, fmt.Errorf("failed checking account meta cache: %w", err)
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

	// doing a "full" fetch from here on
	accountMetaFetches.Inc()

	flags, err := e.Flags.Get(ctx, ident.DID.String())
	if err != nil {
		return nil, fmt.Errorf("failed checking account flag cache: %w", err)
	}

	// fetch account metadata from AppView
	pv, err := appbsky.ActorGetProfile(ctx, e.BskyClient, ident.DID.String())
	if err != nil {
		logger.Warn("account profile lookup failed", "err", err)
		am := AccountMeta{
			Identity: ident,
			// Profile
			// AccountLabels
			// AccountNegatedLabels
			AccountFlags: flags,
		}
		return &am, nil
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

	am := AccountMeta{
		Identity: ident,
		Profile: ProfileSummary{
			HasAvatar:   pv.Avatar != nil,
			Description: pv.Description,
			DisplayName: pv.DisplayName,
		},
		AccountLabels:        dedupeStrings(labels),
		AccountNegatedLabels: dedupeStrings(negLabels),
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
			logger.Warn("failed to fetch private account metadata", "err", err)
		} else {
			ap := AccountPrivate{}
			if pv.Email != nil && *pv.Email != "" {
				ap.Email = *pv.Email
			}
			if pv.EmailConfirmedAt != nil && *pv.EmailConfirmedAt != "" {
				ap.EmailConfirmed = true
			}
			ts, err := syntax.ParseDatetimeTime(pv.IndexedAt)
			if err != nil {
				return nil, fmt.Errorf("bad account IndexedAt: %w", err)
			}
			ap.IndexedAt = ts
			am.Private = &ap
		}
	}

	val, err := json.Marshal(&am)
	if err != nil {
		return nil, err
	}

	if err := e.Cache.Set(ctx, "acct", ident.DID.String(), string(val)); err != nil {
		logger.Error("writing to account meta cache failed", "err", err)
	}
	return &am, nil
}
