package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
	"github.com/gocql/gocql"
	"github.com/ipfs/go-cid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/qb"
	"github.com/scylladb/gocqlx/v2/table"

	"github.com/urfave/cli/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type PlaybackState struct {
	EnqueuedRepos map[string]*RepoState
	FinishedRepos map[string]*RepoState

	outDir string

	lk          sync.RWMutex
	wg          sync.WaitGroup
	exit        chan struct{}
	workerCount int

	textLen atomic.Uint64

	ses gocqlx.Session
}

func (s *PlaybackState) Dequeue() string {
	s.lk.Lock()
	defer s.lk.Unlock()

	enqueuedJobs.Set(float64(len(s.EnqueuedRepos)))

	for repo, state := range s.EnqueuedRepos {
		if state.State == "enqueued" {
			state.State = "dequeued"
			return repo
		}
	}

	return ""
}

func (s *PlaybackState) Finish(repo string, state string) {
	s.lk.Lock()
	defer s.lk.Unlock()

	s.FinishedRepos[repo] = &RepoState{
		Repo:       repo,
		State:      state,
		FinishedAt: time.Now(),
	}

	finishedJobs.Set(float64(len(s.FinishedRepos)))

	delete(s.EnqueuedRepos, repo)
}

var postMetadata = table.Metadata{
	Name:    "netsync.posts",
	Columns: []string{"did", "rkey", "parent_did", "parent_rkey", "content", "embed", "facets", "self_labels", "created_at"},
	PartKey: []string{"did", "rkey"},
}
var postTable = table.New(postMetadata)

type Post struct {
	Did  string
	Rkey string

	ParentDid  string
	ParentRkey string

	Content    string
	Embed      string
	Facets     string
	SelfLabels []string

	CreatedAt time.Time
}

var postsByDIDMetadata = table.Metadata{
	Name:    "netsync.posts_by_did",
	Columns: []string{"did", "rkey", "created_at"},
	PartKey: []string{"did"},
	SortKey: []string{"created_at"},
}
var postsByDIDTable = table.New(postsByDIDMetadata)

type PostByDID struct {
	Did       string
	Rkey      string
	CreatedAt time.Time
}

var repliesMetadata = table.Metadata{
	Name:    "netsync.replies",
	Columns: []string{"parent_did", "parent_rkey", "child_did", "child_rkey", "created_at"},
	PartKey: []string{"parent_did", "parent_rkey"},
	SortKey: []string{"child_did", "child_rkey"},
}
var repliesTable = table.New(repliesMetadata)

type Reply struct {
	ParentDid  string
	ParentRkey string
	ChildDid   string
	ChildRkey  string
	CreatedAt  time.Time
}

var followsMetadata = table.Metadata{
	Name:    "netsync.follows",
	Columns: []string{"actor", "rkey", "target", "created_at"},
	PartKey: []string{"actor", "rkey"},
}
var followsTable = table.New(followsMetadata)

type Follow struct {
	Actor     string
	Rkey      string
	Target    string
	CreatedAt time.Time
}

var followByActorMetadata = table.Metadata{
	Name:    "netsync.follows_by_actor",
	Columns: []string{"actor", "target", "created_at"},
	PartKey: []string{"actor", "target"},
}
var followByActorTable = table.New(followByActorMetadata)

type FollowByActor struct {
	Actor     string
	Target    string
	CreatedAt time.Time
}

var followByTargetMetadata = table.Metadata{
	Name:    "netsync.follows_by_target",
	Columns: []string{"target", "actor", "created_at"},
	PartKey: []string{"target", "actor"},
}
var followByTargetTable = table.New(followByTargetMetadata)

type FollowByTarget struct {
	Target    string
	Actor     string
	CreatedAt time.Time
}

var blocksMetadata = table.Metadata{
	Name:    "netsync.blocks",
	Columns: []string{"actor", "rkey", "target", "created_at"},
	PartKey: []string{"actor", "rkey"},
}
var blocksTable = table.New(blocksMetadata)

type Block struct {
	Actor     string
	Rkey      string
	Target    string
	CreatedAt time.Time
}

var blockByActorMetadata = table.Metadata{
	Name:    "netsync.blocks_by_actor",
	Columns: []string{"actor", "target", "created_at"},
	PartKey: []string{"actor", "target"},
}
var blockByActorTable = table.New(blockByActorMetadata)

type BlockByActor struct {
	Actor     string
	Target    string
	CreatedAt time.Time
}

var blockByTargetMetadata = table.Metadata{
	Name:    "netsync.blocks_by_target",
	Columns: []string{"target", "actor", "created_at"},
	PartKey: []string{"target", "actor"},
}
var blockByTargetTable = table.New(blockByTargetMetadata)

type BlockByTarget struct {
	Target    string
	Actor     string
	CreatedAt time.Time
}

var likesMetadata = table.Metadata{
	Name:    "netsync.likes",
	Columns: []string{"did", "rkey", "subject", "created_at"},
	PartKey: []string{"did", "rkey"},
}
var likesTable = table.New(likesMetadata)

type Like struct {
	Did       string
	Rkey      string
	Subject   string
	CreatedAt time.Time
}

var likeCountMetadata = table.Metadata{
	Name:    "netsync.like_counts",
	Columns: []string{"did", "nsid", "rkey", "count"},
	PartKey: []string{"did", "nsid", "rkey"},
}
var likeCountTable = table.New(likeCountMetadata)

type LikeCount struct {
	Did   string
	Nsid  string
	Rkey  string
	Count int64
}

var repostsMetadata = table.Metadata{
	Name:    "netsync.reposts",
	Columns: []string{"did", "rkey", "subject", "created_at"},
	PartKey: []string{"did", "rkey"},
}
var repostsTable = table.New(repostsMetadata)

type Repost struct {
	Did       string
	Rkey      string
	Subject   string
	CreatedAt time.Time
}

var repostCountMetadata = table.Metadata{
	Name:    "netsync.repost_counts",
	Columns: []string{"did", "nsid", "rkey", "count"},
	PartKey: []string{"did", "nsid", "rkey"},
}
var repostCountTable = table.New(repostCountMetadata)

type RepostCount struct {
	Did   string
	Nsid  string
	Rkey  string
	Count int64
}

func (s *PlaybackState) SetupSchema() error {
	if err := s.ses.ExecStmt(`CREATE KEYSPACE IF NOT EXISTS netsync WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : 3 };`); err != nil {
		return fmt.Errorf("failed to create keyspace: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.posts (did text, rkey text, parent_did text, parent_rkey text, content text, embed text, facets text, self_labels list<text>, created_at timestamp, PRIMARY KEY ((did, rkey)));`); err != nil {
		return fmt.Errorf("failed to create posts table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.posts_by_did (did text, rkey text, created_at timestamp, PRIMARY KEY (did, created_at)) WITH CLUSTERING ORDER BY (created_at DESC);`); err != nil {
		return fmt.Errorf("failed to create posts by did table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.replies (parent_did text, parent_rkey text, child_did text, child_rkey text, created_at timestamp, PRIMARY KEY ((parent_did, parent_rkey), child_did, child_rkey));`); err != nil {
		return fmt.Errorf("failed to create replies table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.follows (actor text, rkey text, target text, created_at timestamp, PRIMARY KEY ((actor, rkey)));`); err != nil {
		return fmt.Errorf("failed to create follows table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.follows_by_actor (actor text, rkey text, target text, created_at timestamp, PRIMARY KEY ((actor, target)));`); err != nil {
		return fmt.Errorf("failed to create follows by actor table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.follows_by_target (target text, actor text, created_at timestamp, PRIMARY KEY ((target, actor)));`); err != nil {
		return fmt.Errorf("failed to create follows by target table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.blocks (actor text, rkey text, target text, created_at timestamp, PRIMARY KEY ((actor, rkey)));`); err != nil {
		return fmt.Errorf("failed to create blocks table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.blocks_by_actor (actor text, target text, created_at timestamp, PRIMARY KEY ((actor, target)));`); err != nil {
		return fmt.Errorf("failed to create blocks by actor table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.blocks_by_target (target text, actor text, created_at timestamp, PRIMARY KEY ((target, actor)));`); err != nil {
		return fmt.Errorf("failed to create blocks by target table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.likes (did text, rkey text, subject text, created_at timestamp, PRIMARY KEY ((did, rkey)));`); err != nil {
		return fmt.Errorf("failed to create likes table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.like_counts (did text, nsid text, rkey text, count counter, PRIMARY KEY ((did, nsid, rkey)));`); err != nil {
		return fmt.Errorf("failed to create like counts table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.reposts (did text, rkey text, subject text, created_at timestamp, PRIMARY KEY ((did, rkey)));`); err != nil {
		return fmt.Errorf("failed to create reposts table: %w", err)
	}

	if err := s.ses.ExecStmt(`CREATE TABLE IF NOT EXISTS netsync.repost_counts (did text, nsid text, rkey text, count counter, PRIMARY KEY ((did, nsid, rkey)));`); err != nil {
		return fmt.Errorf("failed to create repost counts table: %w", err)
	}

	return nil
}

func Playback(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	cluster := gocql.NewCluster(cctx.StringSlice("scylla-nodes")...)
	session, err := gocqlx.WrapSession(cluster.CreateSession())
	if err != nil {
		return fmt.Errorf("failed to create scylla session: %w", err)
	}

	state := &PlaybackState{
		outDir:      cctx.String("out-dir"),
		workerCount: cctx.Int("worker-count"),
		wg:          sync.WaitGroup{},
		ses:         session,
	}

	err = state.SetupSchema()
	if err != nil {
		return fmt.Errorf("failed to setup schema: %w", err)
	}

	state.EnqueuedRepos = make(map[string]*RepoState)
	state.FinishedRepos = make(map[string]*RepoState)

	state.exit = make(chan struct{})

	// Start metrics server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cctx.Int("port")),
		Handler: mux,
	}

	go func() {
		state.wg.Add(1)
		defer state.wg.Done()
		if err := metricsServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("failed to start metrics server: %+v", err)
		}
		log.Info("metrics server shut down successfully")
	}()

	// Load all the repos from the out dir
	err = filepath.WalkDir(state.outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk path: %w", err)
		}

		if d.IsDir() {
			return nil
		}

		state.EnqueuedRepos[d.Name()] = &RepoState{
			Repo:  d.Name(),
			State: "enqueued",
		}

		enqueuedJobs.Inc()

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk out dir: %w", err)
	}

	// Start workers
	for i := 0; i < state.workerCount; i++ {
		go state.worker(i)
	}

	// Check for empty queue
	go func() {
		state.wg.Add(1)
		defer state.wg.Done()
		t := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				state.lk.RLock()
				if len(state.EnqueuedRepos) == 0 {
					log.Info("no more repos to process, shutting down")
					close(state.exit)
					return
				}
				state.lk.RUnlock()
			}
		}
	}()

	// Trap SIGINT to trigger a shutdown.
	log.Info("listening for signals")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		cancel()
		close(state.exit)
		log.Infof("shutting down on signal: %+v", sig)
	case <-ctx.Done():
		cancel()
		close(state.exit)
		log.Info("shutting down on context done")
	case <-state.exit:
		cancel()
		log.Info("shutting down on exit signal")
	}

	log.Info("shutting down, waiting for workers to clean up...")

	if err := metricsServer.Shutdown(ctx); err != nil {
		log.Errorf("failed to shut down metrics server: %+v", err)
	}

	state.wg.Wait()

	p := message.NewPrinter(language.English)

	// Print stats
	log.Info(p.Sprintf("processed %d repos and %d UTF-8 text characters in %s",
		len(state.FinishedRepos), state.textLen.Load(), time.Since(start)))
	log.Info("shut down successfully")

	return nil
}

func (s *PlaybackState) worker(id int) {
	log := log.With("worker", id)
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.exit:
			return
		default:
		}

		repo := s.Dequeue()
		if repo == "" {
			return
		}

		processState, err := s.processRepo(context.Background(), repo)
		if err != nil {
			log.Errorf("failed to process repo (%s): %v", repo, err)
		}

		s.Finish(repo, processState)
	}
}

func (s *PlaybackState) processRepo(ctx context.Context, did string) (processState string, err error) {
	log := log.With("repo", did)

	log.Debug("processing repo")

	// Open the repo file from the out dir
	f, err := os.Open(filepath.Join(s.outDir, did))
	if err != nil {
		return "", fmt.Errorf("failed to open repo file: %w", err)
	}
	defer f.Close()

	r, err := repo.ReadRepoFromCar(ctx, f)
	if err != nil {
		return "", fmt.Errorf("failed to read repo from car: %w", err)
	}

	// maxBatchSize := 1000

	// followBatch := s.ses.NewBatch(gocql.LoggedBatch)
	// followBatchSize := 0

	// blockBatch := s.ses.NewBatch(gocql.LoggedBatch)
	// blockBatchSize := 0

	// likeBatch := s.ses.NewBatch(gocql.LoggedBatch)
	// likeBatchSize := 0

	// repostBatch := s.ses.NewBatch(gocql.LoggedBatch)
	// repostBatchSize := 0

	err = r.ForEach(ctx, "", func(path string, _ cid.Cid) error {
		select {
		case <-s.exit:
			return fmt.Errorf("exiting")
		default:
		}

		_, rec, err := r.GetRecord(ctx, path)
		if err != nil {
			return fmt.Errorf("failed to get record: %w", err)
		}

		rkey := strings.Split(path, "/")[1]

		switch rec := rec.(type) {
		case *bsky.FeedPost:
			log.Debugf("processing feed post: %s", rec.Text)
			s.textLen.Add(uint64(len(rec.Text)))
			recCreatedAt, err := dateparse.ParseAny(rec.CreatedAt)
			if err != nil {
				log.Errorf("failed to parse created at: %+v", err)
				return nil
			}

			post := Post{
				Did:       did,
				Rkey:      rkey,
				Content:   rec.Text,
				CreatedAt: recCreatedAt,
			}

			facets := ""
			if rec.Facets != nil && len(rec.Facets) > 0 {
				nonNilFacets := []*bsky.RichtextFacet{}

				// Filter out nil facets
				for i, facet := range rec.Facets {
					for _, feature := range facet.Features {
						if feature.RichtextFacet_Link != nil || feature.RichtextFacet_Mention != nil {
							nonNilFacets = append(nonNilFacets, rec.Facets[i])
							break
						}
					}
				}

				facetBytes, err := json.Marshal(nonNilFacets)
				if err != nil {
					log.Errorf("failed to marshal facets: %+v", err)
					return nil
				}
				facets = string(facetBytes)
			}

			embed := ""
			if rec.Embed != nil {
				// Filter out empty embeds
				if rec.Embed.EmbedExternal != nil ||
					rec.Embed.EmbedImages != nil ||
					rec.Embed.EmbedRecord != nil ||
					rec.Embed.EmbedRecordWithMedia != nil {
					embedBytes, err := json.Marshal(rec.Embed)
					if err != nil {
						log.Errorf("failed to marshal embed: %+v", err)
						return nil
					}
					embed = string(embedBytes)
				}
			}

			selfLabels := []string{}

			if rec.Labels != nil &&
				rec.Labels.LabelDefs_SelfLabels != nil &&
				len(rec.Labels.LabelDefs_SelfLabels.Values) > 0 {
				for _, label := range rec.Labels.LabelDefs_SelfLabels.Values {
					selfLabels = append(selfLabels, label.Val)
				}
			}

			parentParts := []string{}
			if rec.Reply != nil && rec.Reply.Parent != nil {
				// at://did/app.bsky.feed.post/rkey
				parentURI := rec.Reply.Parent.Uri
				parentURI = strings.TrimPrefix(parentURI, "at://")
				parentParts = strings.Split(parentURI, "/")
				if len(parentParts) != 3 {
					log.Errorf("invalid parent URI: %s", parentURI)
					return nil
				}
			}

			if facets != "" {
				post.Facets = facets
			}

			if embed != "" {
				post.Embed = embed
			}

			if len(parentParts) > 0 {
				post.ParentDid = parentParts[0]
				post.ParentRkey = parentParts[2]
			}

			if len(selfLabels) > 0 {
				post.SelfLabels = selfLabels
			}

			// Insert into the DID+RKey lookup table
			insertPost := postTable.InsertQuery(s.ses)
			err = insertPost.BindStruct(&post).ExecRelease()
			if err != nil {
				log.Errorf("failed to bind post: %w", err)
				return nil
			}

			// Insert into post into author-indexed table
			insertPostByDID := postsByDIDTable.InsertQuery(s.ses)
			err = insertPostByDID.BindStruct(&PostByDID{
				Did:       did,
				Rkey:      rkey,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec post by did: %w", err)
				return nil
			}

			// Insert into the reply lookup table
			if len(parentParts) > 0 {
				insertReply := repliesTable.InsertQuery(s.ses)
				err = insertReply.BindStruct(&Reply{
					ParentDid:  parentParts[0],
					ParentRkey: parentParts[2],
					ChildDid:   did,
					ChildRkey:  rkey,
					CreatedAt:  recCreatedAt,
				}).ExecRelease()
				if err != nil {
					log.Errorf("failed to exec reply: %w", err)
					return nil
				}
			}
		case *bsky.FeedLike:
			log.Debugf("processing feed like: %s", rec.Subject.Uri)
			recCreatedAt, err := dateparse.ParseAny(rec.CreatedAt)
			if err != nil {
				log.Errorf("failed to parse created at: %+v", err)
				return nil
			}

			// insertLike := likesTable.InsertQuery(s.ses)
			// err = likeBatch.BindStruct(insertLike, &Like{
			// 	Did:       did,
			// 	Rkey:      rkey,
			// 	Subject:   rec.Subject.Uri,
			// 	CreatedAt: recCreatedAt,
			// })
			// if err != nil {
			// 	log.Errorf("failed to bind like: %w", err)
			// 	return nil
			// }
			// likeBatchSize++

			// Insert into the DID+RKey lookup table
			insertLike := likesTable.InsertQuery(s.ses)
			err = insertLike.BindStruct(&Like{
				Did:       did,
				Rkey:      rkey,
				Subject:   rec.Subject.Uri,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec like: %w", err)
				return nil
			}

			subj := strings.TrimPrefix(rec.Subject.Uri, "at://")
			subjParts := strings.Split(subj, "/")
			if len(subjParts) != 3 {
				log.Errorf("invalid subject: %s", rec.Subject.Uri)
				return nil
			}

			// Increment counter for subject
			updateLikeCount := likeCountTable.UpdateBuilder().
				Add("count").Where(qb.Eq("did"), qb.Eq("nsid"), qb.Eq("rkey")).Query(s.ses).
				BindStruct(&LikeCount{
					Did:   subjParts[0],
					Nsid:  subjParts[1],
					Rkey:  subjParts[2],
					Count: 1,
				})

			err = updateLikeCount.ExecRelease()
			if err != nil {
				log.Errorf("failed to exec like count: %w", err)
				return nil
			}

		case *bsky.FeedRepost:
			log.Debugf("processing feed repost: %s", rec.Subject.Uri)
			recCreatedAt, err := dateparse.ParseAny(rec.CreatedAt)
			if err != nil {
				log.Errorf("failed to parse created at: %+v", err)
				return nil
			}

			// insertRepost := repostsTable.InsertQuery(s.ses)
			// err = repostBatch.BindStruct(insertRepost, &Repost{
			// 	Did:       did,
			// 	Rkey:      rkey,
			// 	Subject:   rec.Subject.Uri,
			// 	CreatedAt: recCreatedAt,
			// })
			// if err != nil {
			// 	log.Errorf("failed to bind repost: %w", err)
			// 	return nil
			// }
			// repostBatchSize++

			// Insert into the DID+RKey lookup table
			insertRepost := repostsTable.InsertQuery(s.ses)
			err = insertRepost.BindStruct(&Repost{
				Did:       did,
				Rkey:      rkey,
				Subject:   rec.Subject.Uri,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec repost: %w", err)
				return nil
			}

			subj := strings.TrimPrefix(rec.Subject.Uri, "at://")
			subjParts := strings.Split(subj, "/")
			if len(subjParts) != 3 {
				log.Errorf("invalid subject: %s", rec.Subject.Uri)
				return nil
			}

			// Increment counter for subject
			updateRepostCount := repostCountTable.UpdateBuilder().
				Add("count").Where(qb.Eq("did"), qb.Eq("nsid"), qb.Eq("rkey")).Query(s.ses).
				BindStruct(&RepostCount{
					Did:   subjParts[0],
					Nsid:  subjParts[1],
					Rkey:  subjParts[2],
					Count: 1,
				})

			err = updateRepostCount.ExecRelease()
			if err != nil {
				log.Errorf("failed to exec repost count: %w", err)
				return nil
			}
		case *bsky.GraphFollow:
			log.Debugf("processing graph follow: %s", rec.Subject)
			recCreatedAt, err := dateparse.ParseAny(rec.CreatedAt)
			if err != nil {
				log.Errorf("failed to parse created at: %+v", err)
				return nil
			}

			// Batch
			// insertFollow := followsTable.InsertQuery(s.ses)
			// err = followBatch.BindStruct(insertFollow, &FollowByActor{
			// 	Actor:     did,
			// 	Target:    rec.Subject,
			// 	CreatedAt: recCreatedAt,
			// })
			// if err != nil {
			// 	log.Errorf("failed to bind follow: %w", err)
			// 	return nil
			// }
			// followBatchSize++

			// Insert follow to DID+RKey Lookup table
			insertFollow := followsTable.InsertQuery(s.ses)
			err = insertFollow.BindStruct(&Follow{
				Actor:     did,
				Rkey:      rkey,
				Target:    rec.Subject,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec follow: %w", err)
				return nil
			}

			// Insert follow to Actor Lookup table
			insertFollowByActor := followByActorTable.InsertQuery(s.ses)
			err = insertFollowByActor.BindStruct(&FollowByActor{
				Actor:     did,
				Target:    rec.Subject,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec follow by actor: %w", err)
				return nil
			}

			// Insert follow to Target Lookup table
			insertFollowByTarget := followByTargetTable.InsertQuery(s.ses)
			err = insertFollowByTarget.BindStruct(&FollowByTarget{
				Target:    rec.Subject,
				Actor:     did,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec follow by target: %w", err)
				return nil
			}
		case *bsky.GraphBlock:
			log.Debugf("processing graph block: %s", rec.Subject)
			recCreatedAt, err := dateparse.ParseAny(rec.CreatedAt)
			if err != nil {
				log.Errorf("failed to parse created at: %+v", err)
				return nil
			}

			// Insert follow to DID+RKey Lookup table
			insertBlock := blocksTable.InsertQuery(s.ses)
			err = insertBlock.BindStruct(&Block{
				Actor:     did,
				Rkey:      rkey,
				Target:    rec.Subject,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec block: %w", err)
				return nil
			}

			// Insert block to Actor Lookup table
			insertBlockByActor := blockByActorTable.InsertQuery(s.ses)
			err = insertBlockByActor.BindStruct(&BlockByActor{
				Actor:     did,
				Target:    rec.Subject,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec block by actor: %w", err)
				return nil
			}

			// Insert block to Target Lookup table
			insertBlockByTarget := blockByTargetTable.InsertQuery(s.ses)
			err = insertBlockByTarget.BindStruct(&BlockByTarget{
				Target:    rec.Subject,
				Actor:     did,
				CreatedAt: recCreatedAt,
			}).ExecRelease()
			if err != nil {
				log.Errorf("failed to exec block by target: %w", err)
				return nil
			}
		case *bsky.ActorProfile:
			if rec.DisplayName != nil {
				log.Debugf("processing actor profile: %s", *rec.DisplayName)
			}
		}

		// if followBatchSize >= maxBatchSize {
		// 	err = s.ses.ExecuteBatch(followBatch)
		// 	if err != nil {
		// 		log.Errorf("failed to execute batch: %w", err)
		// 	}
		// 	followBatch = s.ses.NewBatch(gocql.LoggedBatch)
		// 	followBatchSize = 0
		// }

		// if blockBatchSize >= maxBatchSize {
		// 	err = s.ses.ExecuteBatch(blockBatch)
		// 	if err != nil {
		// 		log.Errorf("failed to execute batch: %w", err)
		// 	}
		// 	blockBatch = s.ses.NewBatch(gocql.LoggedBatch)
		// 	blockBatchSize = 0
		// }

		// if likeBatchSize >= maxBatchSize {
		// 	err = s.ses.ExecuteBatch(likeBatch)
		// 	if err != nil {
		// 		log.Errorf("failed to execute batch: %w", err)
		// 	}
		// 	likeBatch = s.ses.NewBatch(gocql.LoggedBatch)
		// 	likeBatchSize = 0
		// }

		// if repostBatchSize >= maxBatchSize {
		// 	err = s.ses.ExecuteBatch(repostBatch)
		// 	if err != nil {
		// 		log.Errorf("failed to execute batch: %w", err)
		// 	}
		// 	repostBatch = s.ses.NewBatch(gocql.LoggedBatch)
		// 	repostBatchSize = 0
		// }

		return nil
	})
	if err != nil {
		return "failed (repo foreach)", fmt.Errorf("failed to process repo: %w", err)
	}

	// if followBatchSize > 0 {
	// 	err = s.ses.ExecuteBatch(followBatch)
	// 	if err != nil {
	// 		return "failed (batch)", fmt.Errorf("failed to execute batch: %w", err)
	// 	}
	// }

	// if blockBatchSize > 0 {
	// 	err = s.ses.ExecuteBatch(blockBatch)
	// 	if err != nil {
	// 		return "failed (batch)", fmt.Errorf("failed to execute batch: %w", err)
	// 	}
	// }

	// if likeBatchSize > 0 {
	// 	err = s.ses.ExecuteBatch(likeBatch)
	// 	if err != nil {
	// 		return "failed (batch)", fmt.Errorf("failed to execute batch: %w", err)
	// 	}
	// }

	// if repostBatchSize > 0 {
	// 	err = s.ses.ExecuteBatch(repostBatch)
	// 	if err != nil {
	// 		return "failed (batch)", fmt.Errorf("failed to execute batch: %w", err)
	// 	}
	// }

	return "finished", nil
}
