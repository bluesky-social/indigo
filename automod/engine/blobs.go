package engine

import (
	"fmt"
	"io"
	"net/http"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// Parses out any blobs from the enclosed record.
//
// TODO: currently this function uses schema-specific logic, and won't work with generic lexicon records. A future version could use the indigo/atproto/data package and the raw record CBOR to extract blobs from arbitrary records
//
// NOTE: for consistency with other RecordContext methods, which don't usually return errors, maybe the error-returning version of this function should be a helper function, or definted on RecordOp, and the RecordContext version should return an empty array on error?
func (c *RecordContext) Blobs() ([]lexutil.LexBlob, error) {

	if c.RecordOp.Action != CreateOp {
		// TODO: should this really error, or return empty array?
		return nil, fmt.Errorf("expected record creation, got: %s", c.RecordOp.Action)
	}

	var blobs []lexutil.LexBlob

	switch c.RecordOp.Collection.String() {
	case "app.bsky.feed.post":
		post, ok := c.RecordOp.Value.(*appbsky.FeedPost)
		if !ok {
			return nil, fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		if post.Embed != nil && post.Embed.EmbedImages != nil {
			for _, eii := range post.Embed.EmbedImages.Images {
				if eii.Image != nil {
					blobs = append(blobs, *eii.Image)
				}
			}
		}
		if post.Embed != nil && post.Embed.EmbedExternal != nil {
			ext := post.Embed.EmbedExternal.External
			if ext != nil && ext.Thumb != nil {
				blobs = append(blobs, *ext.Thumb)
			}
		}
		if post.Embed != nil && post.Embed.EmbedRecordWithMedia != nil {
			media := post.Embed.EmbedRecordWithMedia.Media
			if media != nil && media.EmbedImages != nil {
				for _, eii := range media.EmbedImages.Images {
					if eii.Image != nil {
						blobs = append(blobs, *eii.Image)
					}
				}
			}
			if media != nil && media.EmbedExternal != nil {
				ext := media.EmbedExternal.External
				if ext != nil && ext.Thumb != nil {
					blobs = append(blobs, *ext.Thumb)
				}
			}
		}
	case "app.bsky.actor.profile":
		profile, ok := c.RecordOp.Value.(*appbsky.ActorProfile)
		if !ok {
			return nil, fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		if profile.Avatar != nil {
			blobs = append(blobs, *profile.Avatar)
		}
		if profile.Banner != nil {
			blobs = append(blobs, *profile.Banner)
		}
	case "app.bsky.graph.list":
		list, ok := c.RecordOp.Value.(*appbsky.GraphList)
		if !ok {
			return nil, fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		if list.Avatar != nil {
			blobs = append(blobs, *list.Avatar)
		}
	case "app.bsky.feed.generator":
		generator, ok := c.RecordOp.Value.(*appbsky.FeedGenerator)
		if !ok {
			return nil, fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		if generator.Avatar != nil {
			blobs = append(blobs, *generator.Avatar)
		}
	}
	return blobs, nil
}

func fetchBlob(c *RecordContext, blob lexutil.LexBlob) ([]byte, error) {

	var blobBytes []byte

	// TODO: more robust way to write this?
	xrpcURL := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", c.Account.Identity.PDSEndpoint(), c.Account.Identity.DID, blob.Ref)

	resp, err := http.Get(xrpcURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch blob from PDS. did=%s cid=%s statusCode=%d", c.Account.Identity.DID, blob.Ref, resp.StatusCode)
	}

	blobBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return blobBytes, nil
}
