package helpers

import (
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod/keyword"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/ipfs/go-cid"
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

func mustBlob(t *testing.T, c string) *lexutil.LexBlob {
	t.Helper()
	parsed, err := cid.Decode(c)
	if err != nil {
		t.Fatalf("invalid test CID %q: %v", c, err)
	}
	return &lexutil.LexBlob{
		Ref:      lexutil.LexLink(parsed),
		MimeType: "image/jpeg",
		Size:     1024,
	}
}

func TestExtractPostBlobCIDsPost(t *testing.T) {
	cidA := "bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlumdylagaqa"
	cidB := "bafkreicwamkg77pijyudfbdmskelsnuztr6gp62lqfjv3e3urbs3gxnv2m"

	tests := []struct {
		name     string
		embed    *appbsky.FeedPost_Embed
		expected []string
	}{
		{
			name:     "nil embed",
			embed:    nil,
			expected: []string{},
		},
		{
			name:     "empty embed",
			embed:    &appbsky.FeedPost_Embed{},
			expected: nil,
		},
		{
			name: "images only",
			embed: &appbsky.FeedPost_Embed{
				EmbedImages: &appbsky.EmbedImages{
					Images: []*appbsky.EmbedImages_Image{
						{Image: mustBlob(t, cidA)},
						{Image: mustBlob(t, cidB)},
					},
				},
			},
			expected: []string{cidA, cidB},
		},
		{
			name: "recordWithMedia images",
			embed: &appbsky.FeedPost_Embed{
				EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
					Media: &appbsky.EmbedRecordWithMedia_Media{
						EmbedImages: &appbsky.EmbedImages{
							Images: []*appbsky.EmbedImages_Image{
								{Image: mustBlob(t, cidA)},
							},
						},
					},
				},
			},
			expected: []string{cidA},
		},
		{
			name: "gallery only",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidA)}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidB)}},
					},
				},
			},
			expected: []string{cidA, cidB},
		},
		{
			name: "gallery with nil EmbedGallery_Image element",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidA)}},
						{EmbedGallery_Image: nil},
					},
				},
			},
			expected: []string{cidA},
		},
		{
			name: "gallery with nil Image blob",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidA)}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: nil}},
					},
				},
			},
			expected: []string{cidA},
		},
		{
			name: "images and gallery with duplicate CID are deduped",
			embed: &appbsky.FeedPost_Embed{
				EmbedImages: &appbsky.EmbedImages{
					Images: []*appbsky.EmbedImages_Image{
						{Image: mustBlob(t, cidA)},
					},
				},
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidA)}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Image: mustBlob(t, cidB)}},
					},
				},
			},
			expected: []string{cidA, cidB},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert := assert.New(t)
			post := &appbsky.FeedPost{
				Text:  "irrelevant",
				Embed: tc.embed,
			}
			got := ExtractPostBlobCIDsPost(post)
			assert.ElementsMatch(tc.expected, got)
		})
	}
}

func TestExtractTextTokensPost(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		embed        *appbsky.FeedPost_Embed
		expectedText string
	}{
		{
			name:         "text only, no embed",
			text:         "hello world",
			embed:        nil,
			expectedText: "hello world",
		},
		{
			name: "text plus image alt",
			text: "hello",
			embed: &appbsky.FeedPost_Embed{
				EmbedImages: &appbsky.EmbedImages{
					Images: []*appbsky.EmbedImages_Image{
						{Alt: "sunset"},
					},
				},
			},
			expectedText: "hello sunset",
		},
		{
			name: "text plus recordWithMedia image alt",
			text: "hi",
			embed: &appbsky.FeedPost_Embed{
				EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
					Media: &appbsky.EmbedRecordWithMedia_Media{
						EmbedImages: &appbsky.EmbedImages{
							Images: []*appbsky.EmbedImages_Image{
								{Alt: "cat"},
							},
						},
					},
				},
			},
			expectedText: "hi cat",
		},
		{
			name: "text plus gallery alts",
			text: "post",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "one"}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "two"}},
					},
				},
			},
			expectedText: "post one two",
		},
		{
			name: "gallery with nil EmbedGallery_Image element",
			text: "x",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "a"}},
						{EmbedGallery_Image: nil},
					},
				},
			},
			expectedText: "x a",
		},
		{
			name: "gallery item with empty alt is skipped",
			text: "x",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: ""}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "b"}},
					},
				},
			},
			expectedText: "x b",
		},
		{
			name: "combined images and gallery alts",
			text: "start",
			embed: &appbsky.FeedPost_Embed{
				EmbedImages: &appbsky.EmbedImages{
					Images: []*appbsky.EmbedImages_Image{
						{Alt: "img"},
					},
				},
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "g1"}},
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "g2"}},
					},
				},
			},
			expectedText: "start img g1 g2",
		},
		{
			name: "empty post text with gallery alt",
			text: "",
			embed: &appbsky.FeedPost_Embed{
				EmbedGallery: &appbsky.EmbedGallery{
					Items: []*appbsky.EmbedGallery_Items_Elem{
						{EmbedGallery_Image: &appbsky.EmbedGallery_Image{Alt: "only"}},
					},
				},
			},
			expectedText: " only",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert := assert.New(t)
			post := &appbsky.FeedPost{
				Text:  tc.text,
				Embed: tc.embed,
			}
			got := ExtractTextTokensPost(post)
			assert.Equal(keyword.TokenizeText(tc.expectedText), got)
		})
	}
}
