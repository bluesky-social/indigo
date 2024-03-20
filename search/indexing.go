package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"

	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

func (s *Server) runPostIndexer(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "runPostIndexer")
	defer span.End()

	// Batch up to 1000 posts at a time, or every 5 seconds
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	var posts []*PostIndexJob
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if len(posts) > 0 {
				err := s.indexPosts(ctx, posts)
				if err != nil {
					s.logger.Error("failed to index posts", "err", err)
				}
				posts = posts[:0]
			}
		case job := <-s.postQueue:
			posts = append(posts, job)
			if len(posts) >= 1000 {
				err := s.indexPosts(ctx, posts)
				if err != nil {
					s.logger.Error("failed to index posts", "err", err)
				}
				posts = posts[:0]
			}
		}
	}
}

func (s *Server) runProfileIndexer(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "runProfileIndexer")
	defer span.End()

	// Batch up to 1000 profiles at a time, or every 5 seconds
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	var profiles []*ProfileIndexJob
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if len(profiles) > 0 {
				err := s.indexProfiles(ctx, profiles)
				if err != nil {
					s.logger.Error("failed to index profiles", "err", err)
				}
				profiles = profiles[:0]
			}
		case job := <-s.profileQueue:
			profiles = append(profiles, job)
			if len(profiles) >= 1000 {
				err := s.indexProfiles(ctx, profiles)
				if err != nil {
					s.logger.Error("failed to index profiles", "err", err)
				}
				profiles = profiles[:0]
			}
		}
	}
}

func (s *Server) deletePost(ctx context.Context, ident *identity.Identity, recordPath string) error {
	ctx, span := tracer.Start(ctx, "deletePost")
	defer span.End()
	span.SetAttributes(attribute.String("repo", ident.DID.String()), attribute.String("path", recordPath))

	logger := s.logger.With("repo", ident.DID, "path", recordPath, "op", "deletePost")

	parts := strings.SplitN(recordPath, "/", 3)
	if len(parts) < 2 {
		logger.Warn("skipping post record with malformed path")
		return nil
	}
	rkey, err := syntax.ParseTID(parts[1])
	if err != nil {
		logger.Warn("skipping post record with non-TID rkey")
		return nil
	}

	docID := fmt.Sprintf("%s_%s", ident.DID.String(), rkey)
	logger.Info("deleting post from index", "docID", docID)
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
		logger.Warn("opensearch indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("indexing error, code=%d", res.StatusCode)
	}
	return nil
}

func (s *Server) indexPosts(ctx context.Context, jobs []*PostIndexJob) error {
	ctx, span := tracer.Start(ctx, "indexPosts")
	defer span.End()
	span.SetAttributes(attribute.Int("num_posts", len(jobs)))

	log := s.logger.With("op", "indexPosts")
	start := time.Now()

	var buf bytes.Buffer
	for i := range jobs {
		job := jobs[i]
		doc := TransformPost(job.record, job.ident, job.rkey, job.rcid.String())
		docBytes, err := json.Marshal(doc)
		if err != nil {
			log.Warn("failed to marshal post", "err", err)
			return err
		}

		indexScript := []byte(fmt.Sprintf(`{"index":{"_id":"%s"}}%s`, doc.DocId(), "\n"))
		docBytes = append(docBytes, "\n"...)

		buf.Grow(len(indexScript) + len(docBytes))
		buf.Write(indexScript)
		buf.Write(docBytes)
	}

	log.Info("indexing posts", "num_posts", len(jobs))

	res, err := s.escli.Bulk(bytes.NewReader(buf.Bytes()), s.escli.Bulk.WithIndex(s.postIndex))
	if err != nil {
		log.Warn("failed to send bulk indexing request", "err", err)
		return fmt.Errorf("failed to send bulk indexing request: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			log.Warn("failed to read bulk indexing response", "err", err)
			return fmt.Errorf("failed to read bulk indexing response: %w", err)
		}
		log.Warn("opensearch bulk indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("bulk indexing error, code=%d", res.StatusCode)
	}

	log.Info("indexed posts", "num_posts", len(jobs), "duration", time.Since(start))

	return nil
}

func (s *Server) indexProfiles(ctx context.Context, jobs []*ProfileIndexJob) error {
	ctx, span := tracer.Start(ctx, "indexProfiles")
	defer span.End()
	span.SetAttributes(attribute.Int("num_profiles", len(jobs)))

	log := s.logger.With("op", "indexProfiles")
	start := time.Now()

	var buf bytes.Buffer
	for i := range jobs {
		job := jobs[i]

		doc := TransformProfile(job.record, job.ident, job.rcid.String())
		docBytes, err := json.Marshal(doc)
		if err != nil {
			log.Warn("failed to marshal profile", "err", err)
			return err
		}

		indexScript := []byte(fmt.Sprintf(`{"index":{"_id":"%s"}}%s`, job.ident.DID.String(), "\n"))
		docBytes = append(docBytes, "\n"...)

		buf.Grow(len(indexScript) + len(docBytes))
		buf.Write(indexScript)
		buf.Write(docBytes)
	}

	log.Info("indexing profiles", "num_profiles", len(jobs))

	res, err := s.escli.Bulk(bytes.NewReader(buf.Bytes()), s.escli.Bulk.WithIndex(s.profileIndex))
	if err != nil {
		log.Warn("failed to send bulk indexing request", "err", err)
		return fmt.Errorf("failed to send bulk indexing request: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			log.Warn("failed to read bulk indexing response", "err", err)
			return fmt.Errorf("failed to read bulk indexing response: %w", err)
		}
		log.Warn("opensearch bulk indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("bulk indexing error, code=%d", res.StatusCode)
	}

	log.Info("indexed profiles", "num_profiles", len(jobs), "duration", time.Since(start))

	return nil
}

// updateProfilePagranks uses the OpenSearch bulk API to update the pageranks for the given DIDs
func (s *Server) updateProfilePageranks(ctx context.Context, dids []syntax.DID, ranks []float64) error {
	ctx, span := tracer.Start(ctx, "updateProfilePageranks")
	defer span.End()
	span.SetAttributes(attribute.Int("num_profiles", len(dids)))

	log := s.logger.With("op", "updateProfilePageranks")

	log.Info("updating profile pageranks")

	if len(dids) != len(ranks) {
		return fmt.Errorf("number of DIDs and ranks must be equal")
	}

	var buf bytes.Buffer
	for i, did := range dids {
		updateScript := map[string]any{
			"script": map[string]any{
				"source": "ctx._source.pagerank = params.pagerank",
				"lang":   "painless",
				"params": map[string]any{
					"pagerank": ranks[i],
				},
			},
		}
		updateScriptJSON, err := json.Marshal(updateScript)
		if err != nil {
			log.Warn("failed to marshal update script", "err", err)
			return err
		}

		updateMetaJSON := []byte(fmt.Sprintf(`{"update":{"_id":"%s"}}%s`, did.String(), "\n"))
		updateScriptJSON = append(updateScriptJSON, "\n"...)

		buf.Grow(len(updateMetaJSON) + len(updateScriptJSON))
		buf.Write(updateMetaJSON)
		buf.Write(updateScriptJSON)
	}

	res, err := s.escli.Bulk(bytes.NewReader(buf.Bytes()), s.escli.Bulk.WithIndex(s.profileIndex))
	if err != nil {
		log.Warn("failed to send bulk indexing request", "err", err)
		return fmt.Errorf("failed to send bulk indexing request: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			log.Warn("failed to read bulk indexing response", "err", err)
			return fmt.Errorf("failed to read bulk indexing response: %w", err)
		}
		log.Warn("opensearch bulk indexing error", "status_code", res.StatusCode, "response", res, "body", string(body))
		return fmt.Errorf("bulk indexing error, code=%d", res.StatusCode)
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
