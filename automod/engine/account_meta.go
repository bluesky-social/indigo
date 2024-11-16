package engine

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
)

var (
	ReviewStateEscalated = "escalated"
	ReviewStateOpen      = "open"
	ReviewStateClosed    = "closed"
	ReviewStateNone      = "none"
)

// information about a repo/account/identity, always pre-populated and relevant to many rules
type AccountMeta struct {
	Identity             *identity.Identity
	Profile              ProfileSummary
	Private              *AccountPrivate
	AccountLabels        []string
	AccountNegatedLabels []string
	AccountFlags         []string
	FollowersCount       int64
	FollowsCount         int64
	PostsCount           int64
	Takendown            bool
	Deactivated          bool
	// best effort public interpretation of account creation timestamp. not always available, and may be inaccurate/inconsistent for now.
	CreatedAt *time.Time
}

type ProfileSummary struct {
	HasAvatar   bool
	AvatarCid   *string
	BannerCid   *string
	Description *string
	DisplayName *string
}

// opaque fingerprints for correlating abusive accounts
type AbuseSignature struct {
	Property string
	Value    string
}

type AccountPrivate struct {
	Email          string
	EmailConfirmed bool
	IndexedAt      *time.Time
	AccountTags    []string
	// ReviewState will be one of ReviewStateEscalated, ReviewStateOpen, ReviewStateClosed, ReviewStateNone, or "" (unknown)
	ReviewState     string
	Appealed        bool
	AbuseSignatures []AbuseSignature
}
