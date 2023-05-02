package main

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	api "github.com/bluesky-social/indigo/api"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/gorilla/websocket"
	lru "github.com/hashicorp/golang-lru"
	"github.com/ipfs/go-cid"
	flatfs "github.com/ipfs/go-ds-flatfs"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	otel "go.opentelemetry.io/otel"

	es "github.com/opensearch-project/opensearch-go/v2"
	esapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"

	cli "github.com/urfave/cli/v2"

	gorm "gorm.io/gorm"
)

var log = logging.Logger("search")

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

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{}
	app.Commands = []*cli.Command{
		elasticCheckCmd,
		searchCmd,
		runCmd,
	}

	app.RunAndExitOnError()
}

type Server struct {
	escli   *es.Client
	db      *gorm.DB
	bgshost string
	xrpcc   *xrpc.Client
	bgsxrpc *xrpc.Client
	plc     *api.PLCServer

	userCache *lru.Cache
}

var runCmd = &cli.Command{
	Name: "run",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Value:   "sqlite://data/thecloud.db",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.StringFlag{
			Name:     "atp-bgs-host",
			Required: true,
			EnvVars:  []string{"ATP_BGS_HOST"},
		},
		&cli.BoolFlag{
			Name:    "readonly",
			EnvVars: []string{"READONLY"},
		},
		&cli.StringFlag{
			Name: "elastic-cert",
		},
		&cli.StringFlag{
			Name:  "plc-host",
			Value: "https://plc.directory",
		},
		&cli.StringFlag{
			Name:  "pds-host",
			Value: "https://bsky.social",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Connecting to database")
		db, err := cliutil.SetupDatabase(cctx.String("database-url"))
		if err != nil {
			return err
		}

		log.Info("Migrating database")
		db.AutoMigrate(&PostRef{})
		db.AutoMigrate(&User{})
		db.AutoMigrate(&LastSeq{})

		log.Infof("Configuring ES client")
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return fmt.Errorf("failed to get elasticsearch: %w", err)
		}

		log.Infof("Configuring HTTP server")
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			log.Error(err)
		}

		xc := &xrpc.Client{
			Host: cctx.String("pds-host"),
		}

		plc := &api.PLCServer{
			Host: cctx.String("plc-host"),
		}

		bgsws := cctx.String("atp-bgs-host")
		if !strings.HasPrefix(bgsws, "ws") {
			return fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
		}

		bgshttp := strings.Replace(bgsws, "ws", "http", 1)
		bgsxrpc := &xrpc.Client{
			Host: bgshttp,
		}

		ucache, _ := lru.New(100000)
		s := &Server{
			escli:     escli,
			db:        db,
			bgshost:   cctx.String("atp-bgs-host"),
			xrpcc:     xc,
			bgsxrpc:   bgsxrpc,
			plc:       plc,
			userCache: ucache,
		}

		e.Use(middleware.CORS())

		e.GET("/search/posts", s.handleSearchRequestPosts)
		e.GET("/search/profiles", s.handleSearchRequestProfiles)

		go func() {
			panic(e.Start(":3999"))
		}()

		if cctx.Bool("readonly") {
			select {}
		} else {
			ctx := context.TODO()
			if err := s.Run(ctx); err != nil {
				return fmt.Errorf("failed to run: %w", err)
			}
		}

		return nil
	},
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

func (s *Server) Run(ctx context.Context) error {
	cur, err := s.getLastCursor()
	if err != nil {
		return fmt.Errorf("get last cursor: %w", err)
	}

	d := websocket.DefaultDialer
	con, _, err := d.Dial(fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", s.bgshost, cur), http.Header{})
	if err != nil {
		return fmt.Errorf("events dial failed: %w", err)
	}

	return events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
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
	})

	return nil
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

func (s *Server) handleSearchRequestPosts(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchRequestPosts")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	out, err := s.SearchPosts(ctx, q)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) handleSearchRequestProfiles(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchRequestProfiles")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	out, err := s.SearchProfiles(ctx, q)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) SearchPosts(ctx context.Context, srch string) ([]PostSearchResult, error) {
	resp, err := doSearchPosts(ctx, s.escli, srch)
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

type ActorSearchResp struct {
	bsky.ActorProfile
	DID string `json:"did"`
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

func (s *Server) handleFromDid(ctx context.Context, did string) (string, error) {
	handle, _, err := api.ResolveDidToHandle(ctx, s.xrpcc, s.plc, did)
	if err != nil {
		return "", err
	}

	return handle, nil
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

func getEsCli(certfi string) (*es.Client, error) {
	user := "elastic"
	if u := os.Getenv("ELASTIC_USERNAME"); u != "" {
		user = u
	}

	addrs := []string{
		"https://192.168.1.221:9200",
	}

	if hosts := os.Getenv("ELASTIC_HOSTS"); hosts != "" {
		addrs = strings.Split(hosts, ",")
	}

	pass := os.Getenv("ELASTIC_PASSWORD")

	var cert []byte
	if certfi != "" {
		b, err := os.ReadFile(certfi)
		if err != nil {
			return nil, err
		}

		cert = b
	}

	cfg := es.Config{
		Addresses: addrs,
		Username:  user,
		Password:  pass,

		CACert: cert,
	}
	escli, err := es.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set up client: %w", err)
	}
	info, err := escli.Info()
	if err != nil {
		return nil, fmt.Errorf("cannot get escli info: %w", err)
	}
	defer info.Body.Close()
	fmt.Println(info)

	return escli, nil
}

var elasticCheckCmd = &cli.Command{
	Name: "elastic-check",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "elastic-cert",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return err
		}

		inf, err := escli.Info()
		if err != nil {
			return fmt.Errorf("failed to get info: %w", err)
		}

		fmt.Println(inf)
		return nil

	},
}

type EsSearchHit struct {
	Index  string          `json:"_index"`
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

type EsSearchHits struct {
	Total struct {
		Value    int
		Relation string
	} `json:"total"`
	MaxScore float64       `json:"max_score"`
	Hits     []EsSearchHit `json:"hits"`
}

type EsSearchResponse struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	// Shards ???
	Hits EsSearchHits `json:"hits"`
}

type UserResult struct {
	Did    string `json:"did"`
	Handle string `json:"handle"`
}

type PostSearchResult struct {
	Tid  string     `json:"tid"`
	Cid  string     `json:"cid"`
	User UserResult `json:"user"`
	Post any        `json:"post"`
}

func doSearchPosts(ctx context.Context, escli *es.Client, q string) (*EsSearchResponse, error) {
	query := map[string]interface{}{
		/*
			"sort": map[string]any{
				"createdAt": map[string]any{
					"order":  "desc",
					"format": "date_nanos",
				},
			},
		*/
		"query": map[string]interface{}{
			"match": map[string]interface{}{
				"text": q,
			},
		},
	}

	return doSearch(ctx, escli, "posts", query)
}

func doSearchProfiles(ctx context.Context, escli *es.Client, q string) (*EsSearchResponse, error) {
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    q,
				"fields":   []string{"description", "displayName", "handle"},
				"operator": "or",
			},
		},
	}

	return doSearch(ctx, escli, "profiles", query)
}

func doSearch(ctx context.Context, escli *es.Client, index string, query interface{}) (*EsSearchResponse, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}

	// Perform the search request.
	res, err := escli.Search(
		escli.Search.WithContext(ctx),
		escli.Search.WithIndex(index),
		escli.Search.WithBody(&buf),
		escli.Search.WithTrackTotalHits(true),
		escli.Search.WithSize(30),
	)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	var out EsSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	return &out, nil
}

var searchCmd = &cli.Command{
	Name: "search",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "elastic-cert",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return err
		}

		var buf bytes.Buffer
		query := map[string]interface{}{
			"query": map[string]interface{}{
				"match": map[string]interface{}{
					"text": cctx.Args().First(),
				},
			},
		}
		if err := json.NewEncoder(&buf).Encode(query); err != nil {
			log.Fatalf("Error encoding query: %s", err)
		}

		// Perform the search request.
		res, err := escli.Search(
			escli.Search.WithContext(context.Background()),
			escli.Search.WithIndex("posts"),
			escli.Search.WithBody(&buf),
			escli.Search.WithTrackTotalHits(true),
			escli.Search.WithPretty(),
		)
		if err != nil {
			log.Fatalf("Error getting response: %s", err)
		}

		fmt.Println(res)
		return nil

	},
}

func OpenBlockstore(dir string) (blockstore.Blockstore, error) {
	fds, err := flatfs.CreateOrOpen(dir, flatfs.IPFS_DEF_SHARD, false)
	if err != nil {
		return nil, err
	}

	return blockstore.NewBlockstoreNoPrefix(fds), nil
}
