package helpers

import (
	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParentOrRootIsDid(t *testing.T) {
	assert := assert.New(t)

	post1 := &appgndr.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appgndr.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/gndr.app.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/gndr.app.feed.post/rkey123",
			},
		},
	}

	post2 := &appgndr.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appgndr.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/gndr.app.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/gndr.app.feed.post/rkey123",
			},
		},
	}

	post3 := &appgndr.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appgndr.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:abc123/gndr.app.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/gndr.app.feed.post/rkey123",
			},
		},
	}

	post4 := &appgndr.FeedPost{
		Text: "some random post that i dreamt up last night, idk",
		Reply: &appgndr.FeedPost_ReplyRef{
			Root: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/gndr.app.feed.post/rkey123",
			},
			Parent: &comatproto.RepoStrongRef{
				Uri: "at://did:plc:321abc/gndr.app.feed.post/rkey123",
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
		"did:web:gndr.app",
		"did:plc:abc123",
	}

	didList2 := []string{
		"did:plc:321cba",
		"did:web:gndr.app",
		"did:plc:123abc",
	}

	assert.True(PostParentOrRootIsAnyDid(post1, didList1))
	assert.False(PostParentOrRootIsAnyDid(post1, didList2))
}

func TestPostMentionsDid(t *testing.T) {
	assert := assert.New(t)

	post := &appgndr.FeedPost{
		Text: "@hailey.at what is upppp also hello to @darthgander.gndr.social",
		Facets: []*appgndr.RichtextFacet{
			{
				Features: []*appgndr.RichtextFacet_Features_Elem{
					{
						RichtextFacet_Mention: &appgndr.RichtextFacet_Mention{
							Did: "did:plc:abc123",
						},
					},
				},
				Index: &appgndr.RichtextFacet_ByteSlice{
					ByteStart: 0,
					ByteEnd:   9,
				},
			},
			{
				Features: []*appgndr.RichtextFacet_Features_Elem{
					{
						RichtextFacet_Mention: &appgndr.RichtextFacet_Mention{
							Did: "did:plc:abc456",
						},
					},
				},
				Index: &appgndr.RichtextFacet_ByteSlice{
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
		"did:web:gndr.app",
		"did:plc:abc456",
	}

	didList2 := []string{
		"did:plc:321cba",
		"did:web:gndr.app",
		"did:plc:123abc",
	}

	assert.True(PostMentionsAnyDid(post, didList1))
	assert.False(PostMentionsAnyDid(post, didList2))
}
