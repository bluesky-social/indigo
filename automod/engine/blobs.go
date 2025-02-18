package engine

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/data"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/carlmjohnson/versioninfo"
)

// Parses out any blobs from the enclosed record.
//
// NOTE: for consistency with other RecordContext methods, which don't usually return errors, maybe the error-returning version of this function should be a helper function, or defined on RecordOp, and the RecordContext version should return an empty array on error?
func (c *RecordContext) Blobs() ([]lexutil.LexBlob, error) {

	if c.RecordOp.Action == DeleteOp {
		return []lexutil.LexBlob{}, nil
	}

	rec, err := data.UnmarshalCBOR(c.RecordOp.RecordCBOR)
	if err != nil {
		return nil, fmt.Errorf("parsing generic record CBOR: %v", err)
	}
	blobs := data.ExtractBlobs(rec)

	// convert from data.Blob to lexutil.LexBlob; plan is to merge these types eventually
	var out []lexutil.LexBlob
	for _, b := range blobs {
		lb := lexutil.LexBlob{
			Ref:      lexutil.LexLink(b.Ref),
			MimeType: b.MimeType,
			Size:     b.Size,
		}
		out = append(out, lb)
	}
	return out, nil
}

func (c *RecordContext) fetchBlob(blob lexutil.LexBlob) ([]byte, error) {

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		blobDownloadDuration.Observe(duration.Seconds())
	}()

	var blobBytes []byte

	// TODO: potential security issue here with malformed or "localhost" PDS endpoint
	pdsEndpoint := c.Account.Identity.PDSEndpoint()
	xrpcURL := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", pdsEndpoint, c.Account.Identity.DID, blob.Ref)

	req, err := http.NewRequest("GET", xrpcURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "indigo-automod/"+versioninfo.Short())
	// TODO: more robust PDS hostname check (eg, future trailing slash or partial path)
	if c.engine.BskyClient.Headers != nil && strings.HasSuffix(pdsEndpoint, ".bsky.network") {
		val, ok := c.engine.BskyClient.Headers["x-ratelimit-bypass"]
		if ok {
			req.Header.Set("x-ratelimit-bypass", val)
		}
	}

	client := c.engine.BlobClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	blobDownloadCount.WithLabelValues(fmt.Sprint(resp.StatusCode)).Inc()
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("failed to fetch blob from PDS. did=%s cid=%s statusCode=%d", c.Account.Identity.DID, blob.Ref, resp.StatusCode)
	}

	blobBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return blobBytes, nil
}
