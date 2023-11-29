package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

// does nothing, just demonstrating function signature
func NoOpIdentityRule(evt *automod.IdentityEvent) error {
	return nil
}
