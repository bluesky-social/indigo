package helpers

import (
	"time"

	"github.com/bluesky-social/indigo/automod"
)

// no accounts exist before this time
var atprotoAccountEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// returns true if account creation timestamp is plausible: not-nil, not in distant past, not in the future
func plausibleAccountCreation(when *time.Time) bool {
	if when == nil {
		return false
	}
	// this is mostly to check for misconfigurations or null values (eg, UNIX epoch zero means "unknown" not actually 1970)
	if !when.After(atprotoAccountEpoch) {
		return false
	}
	// a timestamp in the future would also indicate some misconfiguration
	if when.After(time.Now().Add(time.Hour)) {
		return false
	}
	return true
}

// checks if account was created recently, based on either public or private account metadata. if metadata isn't available at all, or seems bogus, returns 'false'
func AccountIsYoungerThan(c *automod.AccountContext, age time.Duration) bool {
	// TODO: consider swapping priority order here (and below)
	if c.Account.CreatedAt != nil && plausibleAccountCreation(c.Account.CreatedAt) {
		return time.Since(*c.Account.CreatedAt) < age
	}
	if c.Account.Private != nil && plausibleAccountCreation(c.Account.Private.IndexedAt) {
		return time.Since(*c.Account.Private.IndexedAt) < age
	}
	return false
}

// checks if account was *not* created recently, based on either public or private account metadata. if metadata isn't available at all, or seems bogus, returns 'false'
func AccountIsOlderThan(c *automod.AccountContext, age time.Duration) bool {
	if c.Account.CreatedAt != nil && plausibleAccountCreation(c.Account.CreatedAt) {
		return time.Since(*c.Account.CreatedAt) >= age
	}
	if c.Account.Private != nil && plausibleAccountCreation(c.Account.Private.IndexedAt) {
		return time.Since(*c.Account.Private.IndexedAt) >= age
	}
	return false
}
