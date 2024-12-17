package search

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/backfill"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/time/rate"
	gorm "gorm.io/gorm"

	es "github.com/opensearch-project/opensearch-go/v2"
	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

type Indexer struct {
	escli        *es.Client
	postIndex    string
	profileIndex string
	db           *gorm.DB
	relayhost    string
	relayXRPC    *xrpc.Client
	dir          identity.Directory
	echo         *echo.Echo
	logger       *slog.Logger

	bfs *backfill.Gormstore
	bf  *backfill.Backfiller

	enableRepoDiscovery bool

	indexLimiter  *rate.Limiter
	profileQueue  chan *ProfileIndexJob
	postQueue     chan *PostIndexJob
	pagerankQueue chan *PagerankIndexJob
}

type IndexerConfig struct {
	RelayHost           string
	ProfileIndex        string
	PostIndex           string
	Logger              *slog.Logger
	RelaySyncRateLimit  int
	IndexMaxConcurrency int
	DiscoverRepos       bool
	IndexingRateLimit   int
}

type ProfileIndexJob struct {
	ident  *identity.Identity
	record *appbsky.ActorProfile
	rcid   cid.Cid
}

type PostIndexJob struct {
	did    syntax.DID
	record *appbsky.FeedPost
	rcid   cid.Cid
	rkey   string
}

type PagerankIndexJob struct {
	did  syntax.DID
	rank float64
}

func NewIndexer(db *gorm.DB, escli *es.Client, dir identity.Directory, config IndexerConfig) (*Indexer, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}
	logger = logger.With("component", "indexer")

	logger.Info("running database migrations")
	db.AutoMigrate(&LastSeq{})
	db.AutoMigrate(&backfill.GormDBJob{})

	relayWS := config.RelayHost
	if !strings.HasPrefix(relayWS, "ws") {
		return nil, fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
	}

	relayHTTP := strings.Replace(relayWS, "ws", "http", 1)
	relayXRPC := &xrpc.Client{
		Host: relayHTTP,
	}

	limiter := rate.NewLimiter(rate.Limit(config.IndexingRateLimit), 10_000)

	idx := &Indexer{
		escli:               escli,
		profileIndex:        config.ProfileIndex,
		postIndex:           config.PostIndex,
		db:                  db,
		relayhost:           config.RelayHost,
		relayXRPC:           relayXRPC,
		dir:                 dir,
		logger:              logger,
		enableRepoDiscovery: config.DiscoverRepos,

		indexLimiter:  limiter,
		profileQueue:  make(chan *ProfileIndexJob, 1000),
		postQueue:     make(chan *PostIndexJob, 1000),
		pagerankQueue: make(chan *PagerankIndexJob, 1000),
	}

	bfstore := backfill.NewGormstore(db)
	opts := backfill.DefaultBackfillOptions()

	if config.RelaySyncRateLimit > 0 {
		opts.SyncRequestsPerSecond = config.RelaySyncRateLimit
		opts.ParallelBackfills = 2 * config.RelaySyncRateLimit
	} else {
		opts.SyncRequestsPerSecond = 8
	}

	opts.RelayHost = relayHTTP
	if config.IndexMaxConcurrency > 0 {
		opts.ParallelRecordCreates = config.IndexMaxConcurrency
	} else {
		opts.ParallelRecordCreates = 20
	}
	opts.NSIDFilter = "app.bsky."
	bf := backfill.NewBackfiller(
		"search",
		bfstore,
		idx.handleCreateOrUpdate,
		idx.handleCreateOrUpdate,
		idx.handleDelete,
		opts,
	)
	// reuse identity directory (for efficient caching)
	bf.Directory = dir

	idx.bfs = bfstore
	idx.bf = bf

	return idx, nil
}

//go:embed post_schema.json
var palomarPostSchemaJSON string

//go:embed profile_schema.json
var palomarProfileSchemaJSON string

func (idx *Indexer) EnsureIndices(ctx context.Context) error {
	indices := []struct {
		Name       string
		SchemaJSON string
	}{
		{Name: idx.postIndex, SchemaJSON: palomarPostSchemaJSON},
		{Name: idx.profileIndex, SchemaJSON: palomarProfileSchemaJSON},
	}
	for _, index := range indices {
		resp, err := idx.escli.Indices.Exists([]string{index.Name})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		io.ReadAll(resp.Body)
		if resp.IsError() && resp.StatusCode != 404 {
			return fmt.Errorf("failed to check index existence")
		}
		if resp.StatusCode == 404 {
			idx.logger.Warn("creating opensearch index", "index", index.Name)
			if len(index.SchemaJSON) < 2 {
				return fmt.Errorf("empty schema file (go:embed failed)")
			}
			buf := strings.NewReader(index.SchemaJSON)
			resp, err := idx.escli.Indices.Create(
				index.Name,
				idx.escli.Indices.Create.WithBody(buf))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
			if resp.IsError() {
				return fmt.Errorf("failed to create index")
			}
		}
	}
	return nil
}

func (idx *Indexer) runPostIndexer(ctx context.Context) {
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
				err := idx.indexLimiter.WaitN(ctx, len(posts))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexPosts(ctx, posts)
				if err != nil {
					idx.logger.Error("failed to index posts", "err", err)
				}
				posts = posts[:0]
			}
		case job := <-idx.postQueue:
			posts = append(posts, job)
			if len(posts) >= 1000 {
				err := idx.indexLimiter.WaitN(ctx, len(posts))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexPosts(ctx, posts)
				if err != nil {
					idx.logger.Error("failed to index posts", "err", err)
				}
				posts = posts[:0]
			}
		}
	}
}

func (idx *Indexer) runProfileIndexer(ctx context.Context) {
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
				err := idx.indexLimiter.WaitN(ctx, len(profiles))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexProfiles(ctx, profiles)
				if err != nil {
					idx.logger.Error("failed to index profiles", "err", err)
				}
				profiles = profiles[:0]
			}
		case job := <-idx.profileQueue:
			profiles = append(profiles, job)
			if len(profiles) >= 1000 {
				err := idx.indexLimiter.WaitN(ctx, len(profiles))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexProfiles(ctx, profiles)
				if err != nil {
					idx.logger.Error("failed to index profiles", "err", err)
				}
				profiles = profiles[:0]
			}
		}
	}
}

func (idx *Indexer) runPagerankIndexer(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "runPagerankIndexer")
	defer span.End()

	// Batch up to 1000 pageranks at a time, or every 5 seconds
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	var pageranks []*PagerankIndexJob
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if len(pageranks) > 0 {
				err := idx.indexLimiter.WaitN(ctx, len(pageranks))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexPageranks(ctx, pageranks)
				if err != nil {
					idx.logger.Error("failed to index pageranks", "err", err)
				}
				pageranks = pageranks[:0]
			}
		case job := <-idx.pagerankQueue:
			pageranks = append(pageranks, job)
			if len(pageranks) >= 1000 {
				err := idx.indexLimiter.WaitN(ctx, len(pageranks))
				if err != nil {
					idx.logger.Error("failed to wait for rate limiter", "err", err)
					continue
				}
				err = idx.indexPageranks(ctx, pageranks)
				if err != nil {
					idx.logger.Error("failed to index pageranks", "err", err)
				}
				pageranks = pageranks[:0]
			}
		}
	}
}

func (idx *Indexer) deletePost(ctx context.Context, did syntax.DID, recordPath string) error {
	ctx, span := tracer.Start(ctx, "deletePost")
	defer span.End()
	span.SetAttributes(attribute.String("repo", did.String()), attribute.String("path", recordPath))

	logger := idx.logger.With("repo", did, "path", recordPath, "op", "deletePost")

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

	docID := fmt.Sprintf("%s_%s", did.String(), rkey)
	logger.Info("deleting post from index", "docID", docID)
	req := esapi.DeleteRequest{
		Index:      idx.postIndex,
		DocumentID: docID,
		Refresh:    "true",
	}

	err = idx.indexLimiter.Wait(ctx)
	if err != nil {
		logger.Warn("failed to wait for rate limiter", "err", err)
		return err
	}
	res, err := req.Do(ctx, idx.escli)
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

func (idx *Indexer) indexPosts(ctx context.Context, jobs []*PostIndexJob) error {
	ctx, span := tracer.Start(ctx, "indexPosts")
	defer span.End()
	span.SetAttributes(attribute.Int("num_posts", len(jobs)))

	log := idx.logger.With("op", "indexPosts")
	start := time.Now()

	var buf bytes.Buffer
	for i := range jobs {
		job := jobs[i]
		doc := TransformPost(job.record, job.did, job.rkey, job.rcid.String())
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

	res, err := idx.escli.Bulk(bytes.NewReader(buf.Bytes()), idx.escli.Bulk.WithIndex(idx.postIndex))
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

func (idx *Indexer) indexProfiles(ctx context.Context, jobs []*ProfileIndexJob) error {
	ctx, span := tracer.Start(ctx, "indexProfiles")
	defer span.End()
	span.SetAttributes(attribute.Int("num_profiles", len(jobs)))

	log := idx.logger.With("op", "indexProfiles")
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

	res, err := idx.escli.Bulk(bytes.NewReader(buf.Bytes()), idx.escli.Bulk.WithIndex(idx.profileIndex))
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
func (idx *Indexer) indexPageranks(ctx context.Context, pageranks []*PagerankIndexJob) error {
	ctx, span := tracer.Start(ctx, "indexPageranks")
	defer span.End()
	span.SetAttributes(attribute.Int("num_profiles", len(pageranks)))

	log := idx.logger.With("op", "indexPageranks")

	log.Info("updating profile pageranks")

	var buf bytes.Buffer
	for _, pr := range pageranks {
		updateScript := map[string]any{
			"script": map[string]any{
				"source": "ctx._source.pagerank = params.pagerank",
				"lang":   "painless",
				"params": map[string]any{
					"pagerank": pr.rank,
				},
			},
		}
		updateScriptJSON, err := json.Marshal(updateScript)
		if err != nil {
			log.Warn("failed to marshal update script", "err", err)
			return err
		}

		updateMetaJSON := []byte(fmt.Sprintf(`{"update":{"_id":"%s"}}%s`, pr.did.String(), "\n"))
		updateScriptJSON = append(updateScriptJSON, "\n"...)

		buf.Grow(len(updateMetaJSON) + len(updateScriptJSON))
		buf.Write(updateMetaJSON)
		buf.Write(updateScriptJSON)
	}

	res, err := idx.escli.Bulk(bytes.NewReader(buf.Bytes()), idx.escli.Bulk.WithIndex(idx.profileIndex))
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

func (idx *Indexer) updateUserHandle(ctx context.Context, did syntax.DID, handle string) error {
	ctx, span := tracer.Start(ctx, "updateUserHandle")
	defer span.End()
	span.SetAttributes(attribute.String("repo", did.String()), attribute.String("event.handle", handle))

	log := idx.logger.With("repo", did.String(), "op", "updateUserHandle", "handle_from_event", handle)

	err := idx.dir.Purge(ctx, did.AtIdentifier())
	if err != nil {
		log.Warn("failed to purge DID from directory", "err", err)
		return err
	}

	ident, err := idx.dir.LookupDID(ctx, did)
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
		Index:      idx.profileIndex,
		DocumentID: did.String(),
		Body:       bytes.NewReader(b),
	}

	err = idx.indexLimiter.Wait(ctx)
	if err != nil {
		log.Warn("failed to wait for rate limiter", "err", err)
		return err
	}
	res, err := req.Do(ctx, idx.escli)
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
