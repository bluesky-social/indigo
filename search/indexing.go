package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/attribute"

	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

func (s *Server) deletePost(ctx context.Context, ident *identity.Identity, rkey string) error {
	ctx, span := tracer.Start(ctx, "deletePost")
	defer span.End()
	span.SetAttributes(attribute.String("repo", ident.DID.String()), attribute.String("rkey", rkey))

	log := s.logger.With("repo", ident.DID, "rkey", rkey, "op", "deletePost")
	log.Info("deleting post from index")
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
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read indexing response: %w", err)
	}
	if res.IsError() {
		log.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) indexPost(ctx context.Context, ident *identity.Identity, rec *appbsky.FeedPost, path string, rcid cid.Cid) error {
	ctx, span := tracer.Start(ctx, "indexPost")
	defer span.End()
	span.SetAttributes(attribute.String("repo", ident.DID.String()), attribute.String("path", path))

	log := s.logger.With("repo", ident.DID, "path", path, "op", "indexPost")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		log.Warn("skipping post record with malformed path")
		return nil
	}
	rkey, err := syntax.ParseTID(parts[1])
	if err != nil {
		log.Warn("skipping post record with non-TID rkey")
		return nil
	}

	log = log.With("rkey", rkey)

	doc := TransformPost(rec, ident, rkey.String(), rcid.String())
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	log.Debug("indexing post")
	req := esapi.IndexRequest{
		Index:      s.postIndex,
		DocumentID: doc.DocId(),
		Body:       bytes.NewReader(b),
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		log.Warn("failed to send indexing request", "err", err)
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Warn("failed to read indexing response", "err", err)
		return fmt.Errorf("failed to read indexing response: %w", err)
	}
	if res.IsError() {
		log.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) indexProfile(ctx context.Context, ident *identity.Identity, rec *appbsky.ActorProfile, path string, rcid cid.Cid) error {
	ctx, span := tracer.Start(ctx, "indexProfile")
	defer span.End()
	span.SetAttributes(attribute.String("repo", ident.DID.String()), attribute.String("path", path))

	log := s.logger.With("repo", ident.DID, "path", path, "op", "indexProfile")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[1] != "self" {
		log.Warn("skipping indexing non-canonical profile record", "did", ident.DID, "path", path)
		return nil
	}

	log.Info("indexing profile", "handle", ident.Handle)

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

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		log.Warn("failed to send indexing request", "err", err)
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Warn("failed to read indexing response", "err", err)
		return fmt.Errorf("failed to read indexing response: %w", err)
	}
	if res.IsError() {
		log.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) updateUserHandle(ctx context.Context, did syntax.DID, handle string) error {
	ctx, span := tracer.Start(ctx, "updateUserHandle")
	defer span.End()
	span.SetAttributes(attribute.String("repo", did.String()), attribute.String("event.handle", handle))

	log := s.logger.With("repo", did.String(), "op", "updateUserHandle", "handle_from_event", handle)

	err := s.dir.Purge(ctx, did.AtIdentifier())
	if err != nil {
		log.Warn("failed to purge DID from directory", "err", err)
		return err
	}

	ident, err := s.dir.LookupDID(ctx, did)
	if err != nil {
		log.Warn("failed to lookup DID in directory", "err", err)
		return err
	}

	if ident == nil {
		log.Warn("got nil identity from directory")
		return fmt.Errorf("got nil identity from directory")
	}

	log.Info("updating user handle", "handle_from_dir", ident.Handle)
	span.SetAttributes(attribute.String("dir.handle", ident.Handle.String()))

	b, err := json.Marshal(map[string]any{
		"script": map[string]any{
			"source": "ctx._source.handle = params.handle",
			"lang":   "painless",
			"params": map[string]any{
				"handle": ident.Handle,
			},
		},
	})
	if err != nil {
		log.Warn("failed to marshal update script", "err", err)
		return err
	}

	req := esapi.UpdateRequest{
		Index:      s.profileIndex,
		DocumentID: did.String(),
		Body:       bytes.NewReader(b),
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		log.Warn("failed to send indexing request", "err", err)
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Warn("failed to read indexing response", "err", err)
		return fmt.Errorf("failed to read indexing response: %w", err)
	}
	if res.IsError() {
		log.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}
