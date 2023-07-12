package search

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	api "github.com/bluesky-social/indigo/api"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/gorilla/websocket"
	lru "github.com/hashicorp/golang-lru"
	"github.com/ipfs/go-cid"
	flatfs "github.com/ipfs/go-ds-flatfs"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	es "github.com/opensearch-project/opensearch-go/v2"
	gorm "gorm.io/gorm"
)

var log = logging.Logger("search")

type Server struct {
	escli   *es.Client
	db      *gorm.DB
	bgshost string
	xrpcc   *xrpc.Client
	bgsxrpc *xrpc.Client
	plc     *api.PLCServer
	echo    *echo.Echo

	userCache *lru.Cache
}

type PostRef struct {
	gorm.Model
	Cid string
	Tid string `gorm:"index"`
	Uid uint   `gorm:"index"`
}

type User struct {
	gorm.Model
	Did       string `gorm:"index"`
	Handle    string
	LastCrawl string
}

type LastSeq struct {
	ID  uint `gorm:"primarykey"`
	Seq int64
}

func NewServer(db *gorm.DB, escli *es.Client, plcHost, pdsHost, bgsHost string) (*Server, error) {

	log.Info("Migrating database")
	db.AutoMigrate(&PostRef{})
	db.AutoMigrate(&User{})
	db.AutoMigrate(&LastSeq{})

	// TODO: robust client
	xc := &xrpc.Client{
		Host: pdsHost,
	}

	plc := &api.PLCServer{
		Host: plcHost,
	}

	bgsws := bgsHost
	if !strings.HasPrefix(bgsws, "ws") {
		return nil, fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
	}

	bgshttp := strings.Replace(bgsws, "ws", "http", 1)
	bgsxrpc := &xrpc.Client{
		Host: bgshttp,
	}

	ucache, _ := lru.New(100000)
	s := &Server{
		escli:     escli,
		db:        db,
		bgshost:   bgsHost,
		xrpcc:     xc,
		bgsxrpc:   bgsxrpc,
		plc:       plc,
		userCache: ucache,
	}
	return s, nil
}

func (s *Server) getLastCursor() (int64, error) {
	var lastSeq LastSeq
	if err := s.db.Find(&lastSeq).Error; err != nil {
		return 0, err
	}

	if lastSeq.ID == 0 {
		return 0, s.db.Create(&lastSeq).Error
	}

	return lastSeq.Seq, nil
}

func (s *Server) updateLastCursor(curs int64) error {
	return s.db.Model(LastSeq{}).Where("id = 1").Update("seq", curs).Error
}

func (s *Server) RunIndexer(ctx context.Context) error {
	cur, err := s.getLastCursor()
	if err != nil {
		return fmt.Errorf("get last cursor: %w", err)
	}

	d := websocket.DefaultDialer
	con, _, err := d.Dial(fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", s.bgshost, cur), http.Header{})
	if err != nil {
		return fmt.Errorf("events dial failed: %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			if evt.TooBig && evt.Prev != nil {
				log.Errorf("skipping non-genesis too big events for now: %d", evt.Seq)
				return nil
			}

			if evt.TooBig {
				if err := s.processTooBigCommit(ctx, evt); err != nil {
					log.Errorf("failed to process tooBig event: %s", err)
					return nil
				}

				return nil
			}

			r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
			if err != nil {
				log.Errorf("reading repo from car (seq: %d, len: %d): %w", evt.Seq, len(evt.Blocks), err)
				return nil
			}

			for _, op := range evt.Ops {
				ek := repomgr.EventKind(op.Action)
				switch ek {
				case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
					rc, rec, err := r.GetRecord(ctx, op.Path)
					if err != nil {
						e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Cid, evt.Seq, evt.Repo, err)
						log.Error(e)
						return nil
					}

					if lexutil.LexLink(rc) != *op.Cid {
						log.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
						return nil
					}

					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, &rc, rec); err != nil {
						log.Errorf("failed to handle op: %s", err)
						return nil
					}

				case repomgr.EvtKindDeleteRecord:
					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, nil, nil); err != nil {
						log.Errorf("failed to handle delete: %s", err)
						return nil
					}
				}
			}

			return nil

		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			if err := s.updateUserHandle(ctx, evt.Did, evt.Handle); err != nil {
				log.Errorf("failed to update user handle: %s", err)
			}
			return nil
		},
	}

	return events.HandleRepoStream(ctx, con, events.NewConsumerPool(8, 32, rsc.EventHandler))
}

func (s *Server) handleOp(ctx context.Context, op repomgr.EventKind, seq int64, path string, did string, rcid *cid.Cid, rec any) error {
	if op == repomgr.EvtKindCreateRecord || op == repomgr.EvtKindUpdateRecord {

		log.Infof("handling event(%d): %s - %s", seq, did, path)
		u, err := s.getOrCreateUser(ctx, did)
		if err != nil {
			return fmt.Errorf("checking user: %w", err)
		}
		switch rec := rec.(type) {
		case *bsky.FeedPost:
			if err := s.indexPost(ctx, u, rec, path, *rcid); err != nil {
				return fmt.Errorf("indexing post: %w", err)
			}
		case *bsky.ActorProfile:
			if err := s.indexProfile(ctx, u, rec); err != nil {
				return fmt.Errorf("indexing profile: %w", err)
			}
		default:
		}

	} else if op == repomgr.EvtKindDeleteRecord {
		u, err := s.getOrCreateUser(ctx, did)
		if err != nil {
			return err
		}

		switch {
		// TODO: handle profile deletes, its an edge case, but worth doing still
		case strings.Contains(path, "app.bsky.feed.post"):
			if err := s.deletePost(ctx, u, path); err != nil {
				return err
			}
		}

	}

	if seq%50 == 0 {
		if err := s.updateLastCursor(seq); err != nil {
			log.Error("Failed to update cursor: ", err)
		}
	}

	return nil
}

func (s *Server) processTooBigCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	repodata, err := comatproto.SyncGetRepo(ctx, s.bgsxrpc, evt.Repo, "", evt.Commit.String())
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repodata))
	if err != nil {
		return err
	}

	u, err := s.getOrCreateUser(ctx, evt.Repo)
	if err != nil {
		return err
	}

	return r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		if strings.HasPrefix(k, "app.bsky.feed.post") || strings.HasPrefix(k, "app.bsky.actor.profile") {
			rcid, rec, err := r.GetRecord(ctx, k)
			if err != nil {
				log.Errorf("failed to get record from repo checkout: %s", err)
				return nil
			}

			switch rec := rec.(type) {
			case *bsky.FeedPost:
				if err := s.indexPost(ctx, u, rec, k, rcid); err != nil {
					return fmt.Errorf("indexing post: %w", err)
				}
			case *bsky.ActorProfile:
				if err := s.indexProfile(ctx, u, rec); err != nil {
					return fmt.Errorf("indexing profile: %w", err)
				}
			default:
			}

		}
		return nil
	})
}

func (s *Server) SearchPosts(ctx context.Context, srch string, offset, size int) ([]PostSearchResult, error) {
	resp, err := doSearchPosts(ctx, s.escli, srch, offset, size)
	if err != nil {
		return nil, err
	}

	out := []PostSearchResult{}
	for _, r := range resp.Hits.Hits {
		uid, tid, err := decodeDocumentID(r.ID)
		if err != nil {
			return nil, fmt.Errorf("decoding document id: %w", err)
		}

		var p PostRef
		if err := s.db.First(&p, "tid = ? AND uid = ?", tid, uid).Error; err != nil {
			log.Infof("failed to find post in database that is referenced by elasticsearch: %s", r.ID)
			return nil, err
		}

		var u User
		if err := s.db.First(&u, "id = ?", p.Uid).Error; err != nil {
			return nil, err
		}

		var rec map[string]any
		if err := json.Unmarshal(r.Source, &rec); err != nil {
			return nil, err
		}

		out = append(out, PostSearchResult{
			Tid: p.Tid,
			Cid: p.Cid,
			User: UserResult{
				Did:    u.Did,
				Handle: u.Handle,
			},
			Post: &rec,
		})
	}

	return out, nil
}

func (s *Server) getOrCreateUser(ctx context.Context, did string) (*User, error) {
	cu, ok := s.userCache.Get(did)
	if ok {
		return cu.(*User), nil
	}

	var u User
	if err := s.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}
	if u.ID == 0 {
		// TODO: figure out peoples handles
		h, err := s.handleFromDid(ctx, did)
		if err != nil {
			log.Errorw("failed to resolve did to handle", "did", did, "err", err)
		} else {
			u.Handle = h
		}

		u.Did = did
		if err := s.db.Create(&u).Error; err != nil {
			return nil, err
		}
	}

	s.userCache.Add(did, &u)

	return &u, nil
}

var ErrDoneIterating = fmt.Errorf("done iterating")

func encodeDocumentID(uid uint, tid string) string {
	comb := fmt.Sprintf("%d:%s", uid, tid)
	return base32.StdEncoding.EncodeToString([]byte(comb))
}

func decodeDocumentID(docid string) (uint, string, error) {
	dec, err := base32.StdEncoding.DecodeString(docid)
	if err != nil {
		return 0, "", err
	}

	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) < 2 {
		return 0, "", fmt.Errorf("invalid document id: %q", string(dec))
	}

	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", err
	}

	return uint(uid), parts[1], nil
}

func (s *Server) SearchProfiles(ctx context.Context, srch string) ([]*ActorSearchResp, error) {
	resp, err := doSearchProfiles(ctx, s.escli, srch)
	if err != nil {
		return nil, err
	}

	out := []*ActorSearchResp{}
	for _, r := range resp.Hits.Hits {
		uid, err := strconv.Atoi(r.ID)
		if err != nil {
			return nil, err
		}

		var u User
		if err := s.db.First(&u, "id = ?", uid).Error; err != nil {
			return nil, err
		}

		var rec bsky.ActorProfile
		if err := json.Unmarshal(r.Source, &rec); err != nil {
			return nil, err
		}

		out = append(out, &ActorSearchResp{
			ActorProfile: rec,
			DID:          u.Did,
		})
	}

	return out, nil
}

func OpenBlockstore(dir string) (blockstore.Blockstore, error) {
	fds, err := flatfs.CreateOrOpen(dir, flatfs.IPFS_DEF_SHARD, false)
	if err != nil {
		return nil, err
	}

	return blockstore.NewBlockstoreNoPrefix(fds), nil
}

type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Message string `json:"msg,omitempty"`
}

func (s *Server) handleHealthCheck(c echo.Context) error {
	if err := s.db.Exec("SELECT 1").Error; err != nil {
		log.Errorf("healthcheck can't connect to database: %v", err)
		return c.JSON(500, HealthStatus{Status: "error", Version: version.Version, Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok", Version: version.Version})
	}
}

func (s *Server) RunAPI(listen string) error {

	log.Infof("Configuring HTTP server")
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method} uri=${uri} status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		code := 500
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		log.Warnw("HTTP request error", "statusCode", code, "path", ctx.Path(), "err", err)
		ctx.Response().WriteHeader(code)
	}

	e.Use(middleware.CORS())
	e.GET("/_health", s.handleHealthCheck)
	e.GET("/search/posts", s.handleSearchRequestPosts)
	e.GET("/search/profiles", s.handleSearchRequestProfiles)
	s.echo = e

	log.Infof("starting search API daemon at: %s", listen)
	return s.echo.Start(listen)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
