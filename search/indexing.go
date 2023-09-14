package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"

	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

func (s *Server) deletePost(ctx context.Context, ident *identity.Identity, rkey string) error {
	s.logger.Info("deleting post from index", "repo", ident.DID, "rkey", rkey)
	docID := fmt.Sprintf("%s_%s", ident.DID.String(), rkey)
	req := esapi.DeleteRequest{
		Index:      s.postIndex,
		DocumentID: docID,
		Refresh:    "true",
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		return fmt.Errorf("failed to delete post: %w", err)
	}
	if res.IsError() {
		s.logger.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res)
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) indexPost(ctx context.Context, ident *identity.Identity, rec *appbsky.FeedPost, path string, rcid cid.Cid) error {

	parts := strings.SplitN(path, "/", 3)
	// TODO: replace with an atproto/syntax package type for TID
	var tidRegex = regexp.MustCompile(`^[234567abcdefghijklmnopqrstuvwxyz]{13}$`)
	if len(parts) != 2 || !tidRegex.MatchString(parts[1]) {
		s.logger.Warn("skipping index post record with weird path/TID", "did", ident.DID, "path", path)
		return nil
	}
	rkey := parts[1]

	// TODO: is this needed? what happens if we try to index w/ invalid timestamp?
	_, err := util.ParseTimestamp(rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("post (%s, %s) had invalid timestamp (%q): %w", ident.DID, rkey, rec.CreatedAt, err)
	}

	doc := TransformPost(rec, ident, rkey, rcid.String())
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	s.logger.Debug("indexing post", "did", ident.DID, "rkey", rkey)
	req := esapi.IndexRequest{
		Index:      s.postIndex,
		DocumentID: doc.DocId(),
		Body:       bytes.NewReader(b),
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	if res.IsError() {
		s.logger.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res)
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) indexProfile(ctx context.Context, ident *identity.Identity, rec *appbsky.ActorProfile, path string, rcid cid.Cid) error {

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[1] != "self" {
		s.logger.Warn("skipping indexing non-canonical profile record", "did", ident.DID, "path", path)
		return nil
	}

	s.logger.Info("indexing profile", "did", ident.DID, "handle", ident.Handle)

	doc := TransformProfile(rec, ident, rcid.String())
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req := esapi.IndexRequest{
		Index:      s.profileIndex,
		DocumentID: ident.DID.String(),
		Body:       bytes.NewReader(b),
	}

	res, err := req.Do(context.Background(), s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	if res.IsError() {
		s.logger.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res)
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) updateUserHandle(ctx context.Context, did, handle string) error {
	b, err := json.Marshal(map[string]any{
		"script": map[string]any{
			"source": "ctx._source.handle = params.handle",
			"lang":   "painless",
			"params": map[string]any{
				"handle": handle,
			},
		},
	})
	if err != nil {
		return err
	}

	req := esapi.UpdateRequest{
		Index:      s.profileIndex,
		DocumentID: did,
		Body:       bytes.NewReader(b),
	}

	res, err := req.Do(context.Background(), s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	if res.IsError() {
		s.logger.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res)
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}
