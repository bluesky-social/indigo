package rules

import (
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"
	"github.com/stretchr/testify/assert"
)

func TestTokenizeText(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		s   string
		out []string
	}{
		{
			s:   "1 'Two' three!",
			out: []string{"1", "two", "three"},
		},
		{
			s:   "  foo1;bar2,baz3...",
			out: []string{"foo1", "bar2", "baz3"},
		},
		{
			s:   "https://example.com/index.html",
			out: []string{"https", "example", "com", "index", "html"},
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, keyword.TokenizeText(fix.s))
	}
}

func TestExtractURL(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		s   string
		out []string
	}{
		{
			s:   "this is a description with example.com mentioned in the middle",
			out: []string{"example.com"},
		},
		{
			s:   "this is another example with https://en.wikipedia.org/index.html: and archive.org, and https://eff.org/... and bsky.app.",
			out: []string{"https://en.wikipedia.org/index.html", "archive.org", "https://eff.org/", "bsky.app"},
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, ExtractTextURLs(fix.s))
	}
}

func TestHashOfString(t *testing.T) {
	assert := assert.New(t)

	// hashing function should be consistent over time
	assert.Equal("4e6f69c0e3d10992", HashOfString("dummy-value"))
}

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

func TestParentOrRootIsDid(t *testing.T) {
	assert := assert.New(t)

	post1 := &appbsky.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appbsky.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/app.bsky.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/app.bsky.feed.post/rkey123",
			},
		},
	}

	post2 := &appbsky.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appbsky.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/app.bsky.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/app.bsky.feed.post/rkey123",
			},
		},
	}

	post3 := &appbsky.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appbsky.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/app.bsky.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/app.bsky.feed.post/rkey123",
			},
		},
	}

	post4 := &appbsky.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appbsky.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/app.bsky.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/app.bsky.feed.post/rkey123",
			},
		},
	}

	assert.True(PostParentOrRootIsDid(post1, "did:plc:abc123"))
	assert.False(PostParentOrRootIsDid(post1, "did:plc:321abc"))

	assert.True(PostParentOrRootIsDid(post2, "did:plc:abc123"))
	assert.True(PostParentOrRootIsDid(post2, "did:plc:321abc"))

	assert.True(PostParentOrRootIsDid(post3, "did:plc:abc123"))
	assert.True(PostParentOrRootIsDid(post3, "did:plc:321abc"))

	assert.False(PostParentOrRootIsDid(post4, "did:plc:abc123"))
	assert.True(PostParentOrRootIsDid(post4, "did:plc:321abc"))

	didList1 := []string{
		"did:plc:cba321",
		"did:web:bsky.app",
		"did:plc:abc123",
	}

	didList2 := []string{
		"did:plc:321cba",
		"did:web:bsky.app",
		"did:plc:123abc",
	}

	assert.True(PostParentOrRootIsAnyDid(post1, didList1))
	assert.False(PostParentOrRootIsAnyDid(post1, didList2))
}

func TestPostMentionsDid(t *testing.T) {
	assert := assert.New(t)

	post := &appbsky.FeedPost{
		Text: "@hailey.at what is upppp also hello to @darthbluesky.bsky.social",
		Facets: []*appbsky.RichtextFacet{
			{
				Features: []*appbsky.RichtextFacet_Features_Elem{
					{
						RichtextFacet_Mention: &appbsky.RichtextFacet_Mention{
							Did: "did:plc:abc123",
						},
					},
				},
				Index: &appbsky.RichtextFacet_ByteSlice{
					ByteStart: 0,
					ByteEnd:   9,
				},
			},
			{
				Features: []*appbsky.RichtextFacet_Features_Elem{
					{
						RichtextFacet_Mention: &appbsky.RichtextFacet_Mention{
							Did: "did:plc:abc456",
						},
					},
				},
				Index: &appbsky.RichtextFacet_ByteSlice{
					ByteStart: 39,
					ByteEnd:   63,
				},
			},
		},
	}
	assert.True(PostMentionsDid(post, "did:plc:abc123"))
	assert.False(PostMentionsDid(post, "did:plc:cba321"))

	didList1 := []string{
		"did:plc:cba321",
		"did:web:bsky.app",
		"did:plc:abc456",
	}

	didList2 := []string{
		"did:plc:321cba",
		"did:web:bsky.app",
		"did:plc:123abc",
	}

	assert.True(PostMentionsAnyDid(post, didList1))
	assert.False(PostMentionsAnyDid(post, didList2))
}
