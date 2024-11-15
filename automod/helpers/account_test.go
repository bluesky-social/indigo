package helpers

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/stretchr/testify/assert"
)

func TestAccountIsYoungerThan(t *testing.T) {
	assert := assert.New(t)

	am := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
		Profile: automod.ProfileSummary{},
		Private: nil,
	}
	now := time.Now()
	ac := automod.AccountContext{
		Account: am,
	}
	assert.False(AccountIsYoungerThan(&ac, time.Hour))
	assert.False(AccountIsOlderThan(&ac, time.Hour))

	ac.Account.CreatedAt = &now
	assert.True(AccountIsYoungerThan(&ac, time.Hour))
	assert.False(AccountIsOlderThan(&ac, time.Hour))

	yesterday := time.Now().Add(-1 * time.Hour * 24)
	ac.Account.CreatedAt = &yesterday
	assert.False(AccountIsYoungerThan(&ac, time.Hour))
	assert.True(AccountIsOlderThan(&ac, time.Hour))

	old := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	ac.Account.CreatedAt = &old
	assert.False(AccountIsYoungerThan(&ac, time.Hour))
	assert.False(AccountIsYoungerThan(&ac, time.Hour*24*365*100))
	assert.False(AccountIsOlderThan(&ac, time.Hour))
	assert.False(AccountIsOlderThan(&ac, time.Hour*24*365*100))

	future := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	ac.Account.CreatedAt = &future
	assert.False(AccountIsYoungerThan(&ac, time.Hour))
	assert.False(AccountIsOlderThan(&ac, time.Hour))

	ac.Account.CreatedAt = nil
	ac.Account.Private = &automod.AccountPrivate{
		Email:     "account@example.com",
		IndexedAt: &yesterday,
	}
	assert.True(AccountIsYoungerThan(&ac, 48*time.Hour))
	assert.False(AccountIsYoungerThan(&ac, time.Hour))
	assert.True(AccountIsOlderThan(&ac, time.Hour))
	assert.False(AccountIsOlderThan(&ac, 48*time.Hour))
}
