package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"

	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

func (s *Server) deletePost(ctx context.Context, u *User, path string) error {
	s.logger.Info("deleting post from index", "path", path) // TODO: repo DID
	req := esapi.DeleteRequest{
		Index:      s.postIndex,
		DocumentID: encodeDocumentID(u.ID, path),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		return fmt.Errorf("failed to delete post: %w", err)
	}

	fmt.Println(res)

	return nil
}

func (s *Server) indexPost(ctx context.Context, u *User, rec *bsky.FeedPost, path string, pcid cid.Cid) error {

	parts := strings.SplitN(path, "/", 3)
	var tidRegex = regexp.MustCompile(`^[234567abcdefghijklmnopqrstuvwxyz]{13}$`)
	if len(parts) != 2 || !tidRegex.MatchString(parts[1]) {
		s.logger.Warn("skipping index post record with weird path/TID", "did", u.Did, "path", path)
		return nil
	}
	rkey := parts[1]

	// TODO: just skip this part?
	if err := s.db.Create(&PostRef{
		Cid: pcid.String(),
		Tid: rkey,
		Uid: u.ID,
	}).Error; err != nil {
		return err
	}

	// TODO: is this needed? what happens if we try to index w/ invalid timestamp?
	_, err := time.Parse(util.ISO8601, rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("post (%d, %s) had invalid timestamp (%q): %w", u.ID, rkey, rec.CreatedAt, err)
	}

	doc := TransformPost(rec, u, rkey, pcid.String())
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	s.logger.Debug("indexing post") // TODO: more info
	req := esapi.IndexRequest{
		Index:      s.postIndex,
		DocumentID: doc.DocId(),
		Body:       bytes.NewReader(b),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}

	fmt.Println(res)

	return nil
}

func (s *Server) indexProfile(ctx context.Context, u *User, rec *bsky.ActorProfile, path string, pcid cid.Cid) error {

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[1] != "self" {
		s.logger.Warn("skipping indexing non-canonical profile record", "did", u.Did, "path", path)
		return nil
	}

	n := ""
	if rec.DisplayName != nil {
		n = *rec.DisplayName
	}
	s.logger.Info("indexing profile", "display_name", n)

	doc := TransformProfile(rec, u, pcid.String())
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req := esapi.IndexRequest{
		Index:      s.profileIndex,
		DocumentID: fmt.Sprint(u.ID),
		Body:       bytes.NewReader(b),
		Refresh:    "true",
	}

	res, err := req.Do(context.Background(), s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	fmt.Println(res)

	return nil
}

func (s *Server) updateUserHandle(ctx context.Context, did, handle string) error {
	u, err := s.getOrCreateUser(ctx, did)
	if err != nil {
		return err
	}

	if err := s.db.Model(User{}).Where("id = ?", u.ID).Update("handle", handle).Error; err != nil {
		return err
	}

	u.Handle = handle

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
		DocumentID: fmt.Sprint(u.ID),
		Body:       bytes.NewReader(b),
		Refresh:    "true",
	}

	res, err := req.Do(context.Background(), s.escli)
	if err != nil {
		return fmt.Errorf("failed to send indexing request: %w", err)
	}
	fmt.Println(res)

	return nil
}
