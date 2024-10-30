package helpers

import (
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
