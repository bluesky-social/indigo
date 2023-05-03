package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"

	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

func (s *Server) deletePost(ctx context.Context, u *User, path string) error {
	log.Infof("deleting post: %s", path)
	req := esapi.DeleteRequest{
		Index:      "posts",
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

func (s *Server) indexPost(ctx context.Context, u *User, rec *bsky.FeedPost, tid string, pcid cid.Cid) error {
	if err := s.db.Create(&PostRef{
		Cid: pcid.String(),
		Tid: tid,
		Uid: u.ID,
	}).Error; err != nil {
		return err
	}

	ts, err := time.Parse(util.ISO8601, rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("post (%d, %s) had invalid timestamp (%q): %w", u.ID, tid, rec.CreatedAt, err)
	}

	blob := map[string]any{
		"text":      rec.Text,
		"createdAt": ts.UnixNano(),
		"user":      u.Handle,
	}
	b, err := json.Marshal(blob)
	if err != nil {
		return err
	}

	log.Infof("Indexing post")
	req := esapi.IndexRequest{
		Index:      "posts",
		DocumentID: encodeDocumentID(u.ID, tid),
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

func (s *Server) indexProfile(ctx context.Context, u *User, rec *bsky.ActorProfile) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	n := ""
	if rec.DisplayName != nil {
		n = *rec.DisplayName
	}

	blob := map[string]string{
		"displayName": n,
		"handle":      u.Handle,
		"did":         u.Did,
	}

	if rec.Description != nil {
		blob["description"] = *rec.Description
	}

	log.Infof("Indexing profile: %s", n)
	req := esapi.IndexRequest{
		Index:      "profiles",
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
		Index:      "profiles",
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
