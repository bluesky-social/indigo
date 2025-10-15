package main

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestNSIDNames(t *testing.T) {
	assert := assert.New(t)

	testVectors := [][]string{
		{"app.bsky.feed.post", "appbsky", "FeedPost"},
		{"com.atproto.admin.deleteAccount", "comatproto", "AdminDeleteAccount"},
		{"uk.ac.school.lab.COOL.project", "ukacschool", "LabCoolProject"},
	}

	for _, vec := range testVectors {
		nsid := syntax.NSID(vec[0])
		assert.Equal(vec[1], nsidPkgName(nsid))
		assert.Equal(vec[2], nsidBaseName(nsid))
	}
}
