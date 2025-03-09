package main

import (
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func extractDomain(nsid syntax.NSID) (string, error) {
	// TODO: might have additional atproto-specific tweaks here in the future (for stuff which isn't in PSL yet)
	return publicsuffix.EffectiveTLDPlusOne(nsid.Authority())
}

func extractGroup(nsid syntax.NSID) (string, error) {
	parts := strings.Split(string(nsid), ".")
	group := strings.ToLower(strings.Join(parts[:len(parts)-1], "."))
	return group, nil
}
