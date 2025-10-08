package bsky

import (
	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
)

func (fp *FeedPost) GetEmbedRecord() (*atproto.RepoStrongRef, bool) {
	if fp.Embed != nil && fp.Embed.EmbedRecord != nil && fp.Embed.EmbedRecord.Record != nil {
		return fp.Embed.EmbedRecord.Record, true
	}

	return nil, false
}

func (fp *FeedPost) GetEmbedRecordWithMediaRecord() (*atproto.RepoStrongRef, bool) {
	if fp.Embed != nil && fp.Embed.EmbedRecordWithMedia != nil && fp.Embed.EmbedRecordWithMedia.Record != nil && fp.Embed.EmbedRecordWithMedia.Record.Record != nil {
		return fp.Embed.EmbedRecordWithMedia.Record.Record, true
	}

	return nil, false
}

func (fp *FeedPost) GetEmbedRecordWithMediaMedia() (*bsky.EmbedRecordWithMedia_Media, bool) {
	if fp.Embed != nil && fp.Embed.EmbedRecordWithMedia != nil && fp.Embed.EmbedRecordWithMedia.Media != nil {
		return fp.Embed.EmbedRecordWithMedia.Media, true
	}

	return nil, false
}

func (fp *FeedPost) GetReplyParentUri() (string, bool) {
	if fp.Reply != nil && fp.Reply.Parent != nil {
		return fp.Reply.Parent.Uri, true
	}

	return "", false
}
