package engine

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"

	"github.com/carlmjohnson/versioninfo"
)

// Parses out any blobs from the enclosed record.
//
// TODO: currently this function uses schema-specific logic, and won't work with generic lexicon records. A future version could use the indigo/atproto/data package and the raw record CBOR to extract blobs from arbitrary records
//
// NOTE: for consistency with other RecordContext methods, which don't usually return errors, maybe the error-returning version of this function should be a helper function, or definted on RecordOp, and the RecordContext version should return an empty array on error?
func (c *RecordContext) Blobs() ([]lexutil.LexBlob, error) {

	if c.RecordOp.Action == DeleteOp {
		return []lexutil.LexBlob{}, nil
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

func (c *RecordContext) fetchBlob(blob lexutil.LexBlob) ([]byte, error) {

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		blobDownloadDuration.Observe(duration.Seconds())
	}()

	var blobBytes []byte

	// TODO: better way to do this, eg a shared client?
	client := util.RobustHTTPClient()
	pdsEndpoint := c.Account.Identity.PDSEndpoint()
	xrpcURL := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", pdsEndpoint, c.Account.Identity.DID, blob.Ref)

	req, err := http.NewRequest("GET", xrpcURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "indigo-automod/"+versioninfo.Short())
	// TODO: more robust PDS hostname check
	if c.engine.BskyClient.Headers != nil && strings.HasSuffix(pdsEndpoint, ".bsky.network") {
		val, ok := c.engine.BskyClient.Headers["x-ratelimit-bypass"]
		if ok {
			req.Header.Set("x-ratelimit-bypass", val)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	blobDownloadCount.WithLabelValues(fmt.Sprint(resp.StatusCode)).Inc()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch blob from PDS. did=%s cid=%s statusCode=%d", c.Account.Identity.DID, blob.Ref, resp.StatusCode)
	}

	blobBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return blobBytes, nil
}
