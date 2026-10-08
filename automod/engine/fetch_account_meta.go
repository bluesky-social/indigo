package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

var newAccountRetryDuration = 2 * 1000 * time.Millisecond

// Helper to hydrate metadata about an account from several sources: PDS (if access), mod service (if access), public identity resolution
func (e *Engine) GetAccountMeta(ctx context.Context, ident *identity.Identity) (*AccountMeta, error) {

	logger := e.Logger.With("did", ident.DID.String())

	// fallback in case upstream clients are not configured (eg, testing)
	if e.BskyClient == nil && e.OzoneClient == nil {
		logger.Debug("skipping account meta hydration")
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
	fetchedAnyAccountData := false
	accountMetaFetches.Inc()
	am := AccountMeta{
		Identity: ident,
	}

	// automod-internal "flags"
	flags, err := e.Flags.Get(ctx, ident.DID.String())
	if err != nil {
		return nil, fmt.Errorf("failed checking account flag cache: %w", err)
	}
	am.AccountFlags = flags

	// first attempt to fetch private account metadata from Ozone.
	// this can also include public labels, but does not include public profile info (eg, display name)
	if e.OzoneClient != nil {
		rd, err := toolsozone.ModerationGetRepo(ctx, e.OzoneClient, ident.DID.String())
		if err != nil {
			logger.Warn("failed to fetch private account metadata from Ozone", "err", err)
			var xrpcError *xrpc.Error
			if errors.As(err, &xrpcError) {
				accountOzoneFetches.WithLabelValues(fmt.Sprint(xrpcError.StatusCode)).Inc()
			} else {
				accountOzoneFetches.WithLabelValues("error").Inc()
			}
		} else {
			accountOzoneFetches.WithLabelValues("200").Inc()
			fetchedAnyAccountData = true
			ap := AccountPrivate{}
			if rd.Email != nil && *rd.Email != "" {
				ap.Email = *rd.Email
			}
			if rd.EmailConfirmedAt != nil && *rd.EmailConfirmedAt != "" {
				ap.EmailConfirmed = true
			}
			// TODO: ozone doesn't really return good account "created at", just just leave that field nil
			ap.IndexedAt = nil
			if rd.DeactivatedAt != nil {
				am.Deactivated = true
			}
			if rd.Moderation != nil && rd.Moderation.SubjectStatus != nil {
				if rd.Moderation.SubjectStatus.Takendown != nil && *rd.Moderation.SubjectStatus.Takendown == true {
					am.Takendown = true
				}
				if rd.Moderation.SubjectStatus.Appealed != nil && *rd.Moderation.SubjectStatus.Appealed == true {
					ap.Appealed = true
				}
				ap.AccountTags = dedupeStrings(rd.Moderation.SubjectStatus.Tags)
				if rd.Moderation.SubjectStatus.ReviewState != nil {
					switch *rd.Moderation.SubjectStatus.ReviewState {
					case "tools.ozone.moderation.defs#reviewOpen":
						ap.ReviewState = ReviewStateOpen
					case "tools.ozone.moderation.defs#reviewEscalated":
						ap.ReviewState = ReviewStateEscalated
					case "tools.ozone.moderation.defs#reviewClosed":
						ap.ReviewState = ReviewStateClosed
					case "tools.ozone.moderation.defs#reviewNone":
						ap.ReviewState = ReviewStateNone
					default:
						logger.Warn("unexpected ozone moderation review state", "state", rd.Moderation.SubjectStatus.ReviewState, "did", ident.DID)
					}
				}
			}

			var labels []string
			var negLabels []string
			for _, lbl := range rd.Labels {
				if lbl.Neg != nil && *lbl.Neg == true {
					negLabels = append(negLabels, lbl.Val)
				} else {
					labels = append(labels, lbl.Val)
				}
			}
			am.AccountLabels = dedupeStrings(labels)
			am.AccountNegatedLabels = dedupeStrings(negLabels)

			if rd.ThreatSignatures != nil || len(rd.ThreatSignatures) > 0 {
				asigs := make([]AbuseSignature, len(rd.ThreatSignatures))
				for i, sig := range rd.ThreatSignatures {
					asigs[i] = AbuseSignature{Property: sig.Property, Value: sig.Value}
				}
				ap.AbuseSignatures = asigs
			}

			am.Private = &ap
		}
	}
	// if the ozone private data fetch failed, try to fall back to PDS/entryway fetching. this has less metadata available, but sometimes catches new account creation
	// TODO: this only makes sense for accounts hosted by the infra operator, and using an entryway. aka, pretty bluesky-specific, and will error on all external accounts
	if am.Private == nil && e.AdminClient != nil {
		ai, err := comatproto.AdminGetAccountInfo(ctx, e.AdminClient, ident.DID.String())
		if err != nil {
			logger.Warn("failed to fetch private account metadata from PDS/entryway", "err", err)
			// NOTE: not bothering with metrics on this rare codepath
		} else {
			// NOTE: not setting fetchedAnyAccountData; these results won't get cached in isolation
			ap := AccountPrivate{}
			if ai.Email != nil && *ai.Email != "" {
				ap.Email = *ai.Email
			}
			if ai.EmailConfirmedAt != nil && *ai.EmailConfirmedAt != "" {
				ap.EmailConfirmed = true
			}
			ts, err := syntax.ParseDatetimeTime(ai.IndexedAt)
			if err != nil {
				logger.Warn("bad PDS/entryway account IndexedAt", "err", err)
			} else {
				ap.IndexedAt = &ts
				if am.CreatedAt == nil {
					am.CreatedAt = &ts
				}
			}
			am.Private = &ap
		}
	}

	// fetch other account metadata from bsky AppView
	// this only gets public info, which can include profile contents as well as public labels
	pv, err := appbsky.ActorGetProfile(ctx, e.BskyClient, ident.DID.String())
	if err != nil {
		logger.Warn("account profile lookup failed (from bsky appview)", "err", err)
		var xrpcError *xrpc.Error
		if errors.As(err, &xrpcError) {
			accountAppviewFetches.WithLabelValues(fmt.Sprint(xrpcError.StatusCode)).Inc()
		} else {
			accountAppviewFetches.WithLabelValues("error").Inc()
		}
	} else {
		accountAppviewFetches.WithLabelValues("200").Inc()
		fetchedAnyAccountData = true
		am.Profile = ProfileSummary{
			HasAvatar:   pv.Avatar != nil,
			AvatarCid:   cidFromCdnUrl(pv.Avatar),
			BannerCid:   cidFromCdnUrl(pv.Banner),
			Description: pv.Description,
			DisplayName: pv.DisplayName,
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

		if len(am.AccountLabels) == 0 && len(am.AccountNegatedLabels) == 0 {
			var labels []string
			var negLabels []string
			for _, lbl := range pv.Labels {
				if lbl.Neg != nil && *lbl.Neg == true {
					negLabels = append(negLabels, lbl.Val)
				} else {
					labels = append(labels, lbl.Val)
				}
			}
			am.AccountLabels = dedupeStrings(labels)
			am.AccountNegatedLabels = dedupeStrings(negLabels)
		}

		if pv.CreatedAt != nil && am.CreatedAt == nil {
			ts, err := syntax.ParseDatetimeTime(*pv.CreatedAt)
			if err != nil {
				logger.Warn("invalid appview profile createdAt timestamp", "err", err, "createdAt", pv.CreatedAt)
			} else {
				am.CreatedAt = &ts
			}
		}
	}

	// skip updating cache if no real data fetched
	if !fetchedAnyAccountData {
		logger.Warn("account metadata fetch didn't get any data; not updating cache")
		return &am, nil
	}

	if am.CreatedAt == nil {
		// TODO: might be safe to remove this legacy/debugging log line
		logger.Warn("account metadata missing CreatedAt time")
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
