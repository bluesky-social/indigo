package main

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/bluesky-social/indigo/atproto/syntax"
	lru "github.com/hashicorp/golang-lru/v2"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
)

var serveCmd = &cli.Command{
	Name: "serve",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "api-listen",
			Value:   ":2510",
			EnvVars: []string{"COLLECTIONS_API_LISTEN"},
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2511",
			EnvVars: []string{"COLLECTIONS_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name:     "pebble",
			Usage:    "path to store pebble db",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "dau-directory",
			Usage:    "directory to store DAU pebble db",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "upstream",
			Usage:   "URL, e.g. wss://bsky.network",
			EnvVars: []string{"COLLECTIONS_UPSTREAM"},
		},
		&cli.StringFlag{
			Name:    "admin-token",
			Usage:   "admin authentication",
			EnvVars: []string{"COLLECTIONS_ADMIN_TOKEN"},
		},
		&cli.Float64Flag{
			Name:  "crawl-qps",
			Usage: "per-PDS crawl queries-per-second limit",
			Value: 100,
		},
		&cli.StringFlag{
			Name:    "ratelimit-header",
			Usage:   "secret for friend PDSes",
			EnvVars: []string{"BSKY_SOCIAL_RATE_LIMIT_SKIP", "RATE_LIMIT_HEADER"},
		},
		&cli.Uint64Flag{
			Name:    "clist-min-dids",
			Usage:   "filter collection list to >= N dids",
			Value:   5,
			EnvVars: []string{"COLLECTIONS_CLIST_MIN_DIDS"},
		},
		&cli.IntFlag{
			Name:    "max-did-collections",
			Usage:   "stop recording new collections per did after it has >= this many collections",
			Value:   1000,
			EnvVars: []string{"COLLECTIONS_MAX_DID_COLLECTIONS"},
		},
		&cli.StringFlag{
			Name:    "sets-json-path",
			Usage:   "file path of JSON file containing static word sets",
			EnvVars: []string{"HEPA_SETS_JSON_PATH", "COLLECTIONS_SETS_JSON_PATH"},
		},
		&cli.BoolFlag{
			Name: "verbose",
		},
	},
	Action: func(cctx *cli.Context) error {
		var server collectionServer
		return server.run(cctx)
	},
}

type BadwordChecker interface {
	HasBadword(string) bool
}

type collectionServer struct {
	ctx context.Context

	// the primary directory, all repos ever and their collections
	pcd *PebbleCollectionDirectory

	// daily-active-user directory, new directory every 00:00:00 UTC
	dauDirectory     *PebbleCollectionDirectory
	dauDirectoryPath string    // currently open dauDirectory, {dauDirectoryDir}/{YYYY}{mm}{dd}.pebble
	dauDay           time.Time // YYYY-MM-DD 00:00:00 UTC
	dauTomorrow      time.Time
	dauDirectoryDir  string

	statsCache        *CollectionStats
	statsCacheWhen    time.Time
	statsCacheLock    sync.Mutex
	statsCacheFresh   sync.Cond
	statsCachePending bool

	// (did,collection) pairs from firehose
	ingestFirehose chan DidCollection
	// (did,collection) pairs from PDS crawl (don't apply to dauDirectory)
	ingestCrawl chan DidCollection

	log *slog.Logger

	AdminToken         string
	ExepctedAuthHeader string
	PerPDSCrawlQPS     float64

	activeCrawls     map[string]activeCrawl
	activeCrawlsLock sync.Mutex

	shutdown chan struct{}

	wg sync.WaitGroup

	ratelimitHeader string

	apiServer     *http.Server
	metricsServer *http.Server

	MinDidsForCollectionList uint64
	MaxDidCollections        int

	didCollectionCounts *lru.Cache[string, int]

	badwords BadwordChecker
}

type activeCrawl struct {
	start time.Time
	stats *CrawlStats
}

func (cs *collectionServer) run(cctx *cli.Context) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	cs.shutdown = make(chan struct{})
	level := slog.LevelInfo
	if cctx.Bool("verbose") {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if cctx.IsSet("ratelimit-header") {
		cs.ratelimitHeader = cctx.String("ratelimit-header")
	}
	if cctx.IsSet("sets-json-path") {
		badwords, err := loadBadwords(cctx.String("sets-json-path"))
		if err != nil {
			return err
		}
		cs.badwords = badwords
	}
	cs.MinDidsForCollectionList = cctx.Uint64("clist-min-dids")
	cs.MaxDidCollections = cctx.Int("max-did-collections")
	cs.ingestFirehose = make(chan DidCollection, 1000)
	cs.ingestCrawl = make(chan DidCollection, 1000)
	var err error
	cs.didCollectionCounts, err = lru.New[string, int](1_000_000) // TODO: configurable LRU size
	if err != nil {
		return fmt.Errorf("lru init, %w", err)
	}
	cs.wg.Add(1)
	go cs.ingestReceiver()
	cs.log = log
	cs.ctx = cctx.Context
	cs.AdminToken = cctx.String("admin-token")
	cs.ExepctedAuthHeader = "Bearer " + cs.AdminToken
	pebblePath := cctx.String("pebble")
	cs.pcd = &PebbleCollectionDirectory{
		log: cs.log,
	}
	err = cs.pcd.Open(pebblePath)
	if err != nil {
		return fmt.Errorf("%s: failed to open pebble db: %w", pebblePath, err)
	}
	cs.dauDirectoryDir = cctx.String("dau-directory")
	if cs.dauDirectoryDir != "" {
		err := cs.openDau()
		if err != nil {
			return err
		}
	}
	cs.statsCacheFresh.L = &cs.statsCacheLock
	errchan := make(chan error, 3)
	apiAddr := cctx.String("api-listen")
	cs.wg.Add(1)
	go func() {
		errchan <- cs.StartApiServer(cctx.Context, apiAddr)
	}()
	metricsAddr := cctx.String("metrics-listen")
	cs.wg.Add(1)
	go func() {
		errchan <- cs.StartMetricsServer(cctx.Context, metricsAddr)
	}()

	upstream := cctx.String("upstream")
	if upstream != "" {
		fh := Firehose{
			Log:  log,
			Host: upstream,
			Seq:  -1,
		}
		seq, seqok, err := cs.pcd.GetSequence()
		if err != nil {
			cs.log.Warn("db get seq", "err", err)
		} else if seqok {
			fh.Seq = seq
		}
		fhevents := make(chan *events.XRPCStreamEvent, 1000)
		cs.wg.Add(1)
		go cs.firehoseThread(&fh, fhevents)
		cs.wg.Add(1)
		go cs.handleFirehose(fhevents)
	}

	select {
	case <-signals:
		log.Info("received shutdown signal")
		go errchanlog(cs.log, "server error", errchan)
		return cs.Shutdown()
	case err := <-errchan:
		if err != nil {
			log.Error("server error", "err", err)
			go errchanlog(cs.log, "server error", errchan)
			return cs.Shutdown()
		}
	}
	return nil
}

func (cs *collectionServer) openDau() error {
	now := time.Now().UTC()
	ymd := now.Format("2006-01-02")
	fname := fmt.Sprintf("d%s.pebble", ymd)
	fpath := filepath.Join(cs.dauDirectoryDir, fname)
	daud := &PebbleCollectionDirectory{
		log: cs.log,
	}
	err := daud.Open(fpath)
	if err != nil {
		return fmt.Errorf("%s: failed to open dau pebble db: %w", fpath, err)
	}
	cs.dauDirectory = daud
	cs.dauDirectoryPath = fpath
	cs.dauDay = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cs.dauTomorrow = cs.dauDay.AddDate(0, 0, 1)
	cs.log.Info("DAU db opened", "path", fpath)
	return nil
}

func errchanlog(log *slog.Logger, msg string, errchan <-chan error) {
	for err := range errchan {
		log.Error(msg, "err", err)
	}
}

func (cs *collectionServer) Shutdown() error {
	close(cs.shutdown)
	go func() {
		cs.log.Info("metrics shutdown start")
		sherr := cs.metricsServer.Shutdown(context.Background())
		cs.log.Info("metrics shutdown", "err", sherr)
	}()
	cs.log.Info("api shutdown start...")
	err := cs.apiServer.Shutdown(context.Background())
	//err := cs.esrv.Shutdown(context.Background())
	cs.log.Info("api shutdown, thread wait...", "err", err)
	cs.wg.Wait()
	cs.log.Info("threads done, db close...")
	ee := cs.pcd.Close()
	if ee != nil {
		cs.log.Error("failed to shutdown pebble", "err", ee)
	}
	cs.log.Info("db done. done.")
	return err
}

// firehoseThreads is responsible for connecting to upstream firehose source
func (cs *collectionServer) firehoseThread(fh *Firehose, fhevents chan<- *events.XRPCStreamEvent) {
	defer cs.wg.Done()
	defer cs.log.Info("firehoseThread exit")
	ctx, cancel := context.WithCancel(cs.ctx)
	go func() {
		<-cs.shutdown
		cancel()
	}()
	err := fh.subscribeWithRedialer(ctx, fhevents)
	if err != nil {
		cs.log.Error("failed to subscribe to redialer", "err", err)
	}
	if fh.Seq >= 0 {
		err := cs.pcd.SetSequence(fh.Seq)
		if err != nil {
			cs.log.Warn("db set seq", "err", err)
		}
	}
}

// handleFirehose consumes XRPCStreamEvent from firehoseThread(), further parses data and applies
func (cs *collectionServer) handleFirehose(fhevents <-chan *events.XRPCStreamEvent) {
	defer cs.wg.Done()
	defer cs.log.Info("handleFirehose exit")
	defer close(cs.ingestFirehose)
	var lastSeq int64
	lastSeqSet := false
	notDone := true
	for notDone {
		select {
		case <-cs.shutdown:
			cs.log.Info("firehose handler shutdown")
			notDone = false
		case evt, ok := <-fhevents:
			if !ok {
				notDone = false
				cs.log.Info("firehose handler closed")
				break
			}
			firehoseReceivedCounter.Inc()
			seq, ok := evt.GetSequence()
			if ok {
				lastSeq = seq
				lastSeqSet = true
			}
			if evt.RepoCommit != nil {
				firehoseCommits.Inc()
				cs.handleCommit(evt.RepoCommit)
			}
		}
	}
	if lastSeqSet {
		cs.pcd.SetSequence(lastSeq)
	}
}

func (cs *collectionServer) handleCommit(commit *comatproto.SyncSubscribeRepos_Commit) {
	for _, op := range commit.Ops {
		// op.Path is collection/rkey
		nsid, _, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			cs.log.Warn("bad op path", "repo", commit.Repo, "err", err)
			return
		}
		firehoseCommitOps.WithLabelValues(op.Action).Inc()
		if op.Action == "create" || op.Action == "update" {
			firehoseDidcSet.Inc()
			cs.ingestFirehose <- DidCollection{
				Did:        commit.Repo,
				Collection: nsid.String(),
			}
		}
	}
}

func (cs *collectionServer) StartMetricsServer(ctx context.Context, addr string) error {
	defer cs.wg.Done()
	defer cs.log.Info("metrics server exit")
	cs.metricsServer = &http.Server{
		Addr:    addr,
		Handler: promhttp.Handler(),
	}
	return cs.metricsServer.ListenAndServe()
}

func (cs *collectionServer) StartApiServer(ctx context.Context, addr string) error {
	defer cs.wg.Done()
	defer cs.log.Info("api server exit")
	var lc net.ListenConfig
	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	e := echo.New()
	e.HideBanner = true

	e.Use(MetricsMiddleware)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	e.GET("/_health", cs.healthz)

	e.GET("/xrpc/com.atproto.sync.listReposByCollection", cs.getDidsForCollection)
	e.GET("/v1/getDidsForCollection", cs.getDidsForCollection)
	e.GET("/v1/listCollections", cs.listCollections)

	// TODO: allow public 'requestCrawl' API?
	//e.GET("/xrpc/com.atproto.sync.requestCrawl", cs.crawlPds)
	//e.POST("/xrpc/com.atproto.sync.requestCrawl", cs.crawlPds)

	// admin auth heador required
	e.POST("/admin/pds/requestCrawl", cs.crawlPds) // same as relay
	e.GET("/admin/crawlStatus", cs.crawlStatus)

	e.Listener = li
	srv := &http.Server{
		Handler: e,
	}
	cs.apiServer = srv
	return srv.Serve(li)
}

const statsCacheDuration = time.Second * 300

func getLimit(c echo.Context, min, defaultLim, max int) int {
	limstr := c.QueryParam("limit")
	if limstr == "" {
		return defaultLim
	}
	lvx, err := strconv.ParseInt(limstr, 10, 64)
	if err != nil {
		return defaultLim
	}
	lv := int(lvx)
	if lv < min {
		return min
	}
	if lv > max {
		return max
	}
	return lv
}

// /xrpc/com.atproto.sync.listReposByCollection?collection={}&cursor={}&limit={50<=N<=1000}
// /v1/getDidsForCollection?collection={}&cursor={}&limit={50<=N<=1000}
//
// returns
// {"dids":["did:A", "..."], "cursor":"opaque text"}
func (cs *collectionServer) getDidsForCollection(c echo.Context) error {
	ctx := c.Request().Context()
	collection := c.QueryParam("collection")
	_, err := syntax.ParseNSID(collection)
	if err != nil {
		return c.String(http.StatusBadRequest, fmt.Sprintf("bad collection nsid, %s", err.Error()))
	}
	cursor := c.QueryParam("cursor")
	limit := getLimit(c, 1, 500, 10_000)
	they, nextCursor, err := cs.pcd.ReadCollection(ctx, collection, cursor, limit)
	if err != nil {
		slog.Error("ReadCollection", "collection", collection, "cursor", cursor, "limit", limit, "err", err)
		return c.String(http.StatusInternalServerError, "oops")
	}
	cs.log.Info("getDidsForCollection", "collection", collection, "cursor", cursor, "limit", limit, "count", len(they), "nextCursor", nextCursor)
	var out comatproto.SyncListReposByCollection_Output
	out.Repos = make([]*comatproto.SyncListReposByCollection_Repo, len(they))
	for i, rec := range they {
		out.Repos[i] = &comatproto.SyncListReposByCollection_Repo{Did: rec.Did}
	}
	if nextCursor != "" {
		out.Cursor = &nextCursor
	}
	return c.JSON(http.StatusOK, out)
}

// return cached collection stats if they're fresh
// return new collection stats if they can be calculated quickly
// return stale cached collection stats if new stats take too long
// just wait for fresh stats if there are no cached stats
// stalenessAllowed is how old stats can be before we try to recalculate them, 0=default of 5 minutes
func (cs *collectionServer) getStatsCache(stalenessAllowed time.Duration) (*CollectionStats, error) {
	if stalenessAllowed <= 0 {
		stalenessAllowed = statsCacheDuration
	}
	var statsCache *CollectionStats
	var staleCache *CollectionStats
	var waiter *freshStatsWaiter
	cs.statsCacheLock.Lock()
	if cs.statsCache != nil {
		if time.Since(cs.statsCacheWhen) < stalenessAllowed {
			// has fresh!
			statsCache = cs.statsCache
		} else if !cs.statsCachePending {
			cs.statsCachePending = true
			go cs.statsBuilder()
			staleCache = cs.statsCache
		} else {
			staleCache = cs.statsCache
		}
		if staleCache != nil {
			waiter = &freshStatsWaiter{
				cs:         cs,
				freshCache: make(chan *CollectionStats),
			}
			go waiter.waiter()
		}
	} else if !cs.statsCachePending {
		cs.statsCachePending = true
		go cs.statsBuilder()
	}
	cs.statsCacheLock.Unlock()

	if statsCache != nil {
		// return fresh-enough data
		return statsCache, nil
	}

	if staleCache == nil {
		// block forever waiting for fresh data
		cs.statsCacheLock.Lock()
		for cs.statsCache == nil {
			cs.statsCacheFresh.Wait()
		}
		statsCache = cs.statsCache
		cs.statsCacheLock.Unlock()
		return statsCache, nil
	}

	// wait for up to a second for fresh data, on timeout return stale data
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	select {
	case <-timeout.C:
		cs.statsCacheLock.Lock()
		waiter.l.Lock()
		waiter.obsolete = true
		waiter.l.Unlock()
		cs.statsCacheLock.Unlock()
		return staleCache, nil
	case statsCache = <-waiter.freshCache:
		return statsCache, nil
	}
}

type freshStatsWaiter struct {
	cs         *collectionServer
	l          sync.Mutex
	obsolete   bool
	freshCache chan *CollectionStats
}

func (fsw *freshStatsWaiter) waiter() {
	fsw.cs.statsCacheLock.Lock()
	defer fsw.cs.statsCacheLock.Unlock()
	fsw.cs.statsCacheFresh.Wait()
	fsw.l.Lock()
	defer fsw.l.Unlock()
	if fsw.obsolete {
		close(fsw.freshCache)
	} else {
		fsw.freshCache <- fsw.cs.statsCache
	}
}

func (cs *collectionServer) statsBuilder() {
	for {
		start := time.Now()
		stats, err := cs.pcd.GetCollectionStats()
		dt := time.Since(start)
		if err == nil {
			countsum := uint64(0)
			for _, v := range stats.CollectionCounts {
				countsum += v
			}
			cs.log.Info("stats built", "dt", dt, "total", countsum)
			cs.statsCacheLock.Lock()
			cs.statsCache = &stats
			cs.statsCacheWhen = time.Now()
			cs.statsCacheFresh.Broadcast()
			cs.statsCachePending = false
			cs.statsCacheLock.Unlock()
			return
		} else {
			cs.log.Error("GetCollectionStats", "dt", dt, "err", err)
			time.Sleep(2 * time.Second)
		}
	}
}

func (cs *collectionServer) hasBadword(collection string) bool {
	if cs.badwords != nil {
		return cs.badwords.HasBadword(collection)
	}
	return false
}

// /v1/listCollections?c={}&cursor={}&limit={50<=limit<=1000}
//
// admin may set ?stalesec={} for a maximum number of seconds stale data is accepted
//
// returns
// {"collections":{"app.bsky.feed.post": 123456789, "some collection": 42}, "cursor":"opaque text"}
func (cs *collectionServer) listCollections(c echo.Context) error {
	stalenessAllowed := statsCacheDuration
	stalesecStr := c.QueryParam("stalesec")
	if stalesecStr != "" && cs.isAdmin(c) {
		stalesec, err := strconv.ParseInt(stalesecStr, 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "bad stalesec")
		}
		if stalesec == 0 {
			stalenessAllowed = 1
		} else {
			stalenessAllowed = time.Duration(stalesec) * time.Second
		}
		cs.log.Info("stalesec", "q", stalesecStr, "d", stalenessAllowed)
	}
	stats, err := cs.getStatsCache(stalenessAllowed)
	if err != nil {
		slog.Error("getStatsCache", "err", err)
		return c.String(http.StatusInternalServerError, "oops")
	}
	cursor := c.QueryParam("cursor")
	collections, hasQueryCollections := c.QueryParams()["c"]
	limit := getLimit(c, 50, 500, 1000)
	var out ListCollectionsResponse
	if hasQueryCollections {
		out.Collections = make(map[string]uint64, len(collections))
		for _, collection := range collections {
			count, ok := stats.CollectionCounts[collection]
			if ok {
				out.Collections[collection] = count
			}
		}
	} else {
		allCollections := make([]string, 0, len(stats.CollectionCounts))
		for collection := range stats.CollectionCounts {
			allCollections = append(allCollections, collection)
		}
		sort.Strings(allCollections)
		out.Collections = make(map[string]uint64, limit)
		count := 0
		for _, collection := range allCollections {
			if (cursor == "") || (collection > cursor) {
				if cs.hasBadword(collection) {
					// don't show badwords in public list of collections
					continue
				}
				if stats.CollectionCounts[collection] < cs.MinDidsForCollectionList {
					// don't show experimental/spam collections only implemented by a few DIDs
					continue
				}
				// TODO: probably regex based filter for collection-spam
				out.Collections[collection] = stats.CollectionCounts[collection]
				count++
				if count >= limit {
					out.Cursor = collection
				}
			}
		}
	}
	return c.JSON(http.StatusOK, out)
}

type ListCollectionsResponse struct {
	Collections map[string]uint64 `json:"collections"`
	Cursor      string            `json:"cursor"`
}

func (cs *collectionServer) ingestReceiver() {
	defer cs.wg.Done()
	defer cs.log.Info("ingestReceiver exit")
	errcount := 0
	for {
		select {
		case didc, ok := <-cs.ingestFirehose:
			if !ok {
				cs.log.Info("ingestFirehose closed")
				return
			}
			err := cs.ingestDidc(didc, true)
			if err != nil {
				errcount++
			} else {
				errcount = 0
			}
		case didc := <-cs.ingestCrawl:
			err := cs.ingestDidc(didc, false)
			if err != nil {
				errcount++
			} else {
				errcount = 0
			}
		}
		if errcount > 10 {
			cs.log.Error("ingestReceiver too many errors")
			return // TODO: cancel parent somehow
		}
	}
}

func (cs *collectionServer) ingestDidc(didc DidCollection, dau bool) error {
	count, ok := cs.didCollectionCounts.Get(didc.Did)
	var err error
	if !ok {
		count, err = cs.pcd.CountDidCollections(didc.Did)
		if err != nil {
			return fmt.Errorf("count did collections, %s %w", didc.Did, err)
		}
		cs.didCollectionCounts.Add(didc.Did, count)
	}
	if count >= cs.MaxDidCollections {
		cs.log.Warn("did too many collections", "did", didc.Did)
		return nil
	}
	err = cs.pcd.MaybeSetCollection(didc.Did, didc.Collection)
	if err != nil {
		cs.log.Warn("pcd write", "err", err)
		return err
	}
	if dau && cs.dauDirectory != nil {
		err = cs.maybeDauWrite(didc)
		if err != nil {
			cs.log.Warn("dau write", "err", err)
			return err
		}
	}
	return nil
}

func (cs *collectionServer) maybeDauWrite(didc DidCollection) error {
	now := time.Now()
	if now.After(cs.dauTomorrow) {
		go dauStats(cs.dauDirectory, cs.dauDay, cs.dauDirectoryDir, cs.log)
		cs.dauDirectory = nil
		err := cs.openDau()
		if err != nil {
			return fmt.Errorf("dau reopen, %w", err)
		}
	}
	return cs.dauDirectory.MaybeSetCollection(didc.Did, didc.Collection)
}

// write {dauDirectoryDir}/d{YYYY-MM-DD}.pebble stats summary to {dauDirectoryDir}/d{YYYY-MM-DD}.csv.gz
func dauStats(oldDau *PebbleCollectionDirectory, dauDay time.Time, dauDir string, log *slog.Logger) {
	fname := fmt.Sprintf("d%s.csv.gz", dauDay.Format("2006-01-02"))
	outstatsPath := filepath.Join(dauDir, fname)
	log = log.With("path", outstatsPath)
	log.Info("DAU stats summarize")
	stats, err := oldDau.GetCollectionStats()
	e2 := oldDau.Close()
	if e2 != nil {
		log.Error("old DAU close", "err", e2)
	}
	if err != nil {
		log.Error("old DAU stats", "err", err)
	} else {
		log.Info("DAU stats summarized", "rows", len(stats.CollectionCounts))
		pcdStatsToCsvGz(stats, outstatsPath, log)
	}
}

func pcdStatsToCsvGz(stats CollectionStats, outpath string, log *slog.Logger) {
	fout, err := os.Create(outpath)
	if err != nil {
		log.Error("DAU stats open", "err", err)
		return
	}
	defer fout.Close()
	gzout := gzip.NewWriter(fout)
	defer gzout.Close()
	csvout := csv.NewWriter(gzout)
	defer csvout.Flush()
	err = csvout.Write([]string{"collection", "count"})
	if err != nil {
		log.Error("DAU stats header", "err", err)
		return
	}
	var row [2]string
	rowcount := 0
	for collection, count := range stats.CollectionCounts {
		row[0] = collection
		row[1] = strconv.FormatUint(count, 10)
		err = csvout.Write(row[:])
		if err != nil {
			log.Error("DAU stats row", "err", err)
			return
		}
		rowcount++
	}
	log.Info("DAU stats ok", "rows", rowcount)
}

type CrawlRequest struct {
	Host  string   `json:"hostname,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
}

type CrawlRequestResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func hostOrUrlToUrl(host string) string {
	xu, err := url.Parse(host)
	if err != nil {
		xu = new(url.URL)
		xu.Host = host
		xu.Scheme = "https"
		return xu.String()
	} else if xu.Scheme == "" {
		xu.Scheme = "https"
		return xu.String()
	}
	return host
}

func (cs *collectionServer) isAdmin(c echo.Context) bool {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader == "" {
		return false
	}
	if authHeader == cs.ExepctedAuthHeader {
		return true
	}
	cs.log.Info("wrong auth header", "header", authHeader, "expected", cs.ExepctedAuthHeader)
	return false
}

// /admin/pds/requestCrawl
// same API signature as relay admin requestCrawl
// starts a crawl and returns. See /v1/crawlStatus
// requires header `Authorization: Bearer {admin token}`
//
// POST {"hostname":"one hostname or URL", "hosts":["up to 1000 hosts", "..."]}
// OR
// POST /admin/pds/requestCrawl?hostname={one host}
func (cs *collectionServer) crawlPds(c echo.Context) error {
	isAdmin := cs.isAdmin(c)
	if !isAdmin {
		return c.JSON(http.StatusForbidden, CrawlRequestResponse{Error: "nope"})
	}
	hostQ := c.QueryParam("host")
	if hostQ != "" {
		go cs.crawlThread(hostQ)
		return c.JSON(http.StatusOK, CrawlRequestResponse{Message: "ok"})
	}

	var req CrawlRequest
	err := c.Bind(&req)
	if err != nil {
		cs.log.Info("bad crawl bind", "err", err)
		return c.String(http.StatusBadRequest, err.Error())
	}
	if req.Host != "" {
		go cs.crawlThread(req.Host)
	}
	for _, host := range req.Hosts {
		go cs.crawlThread(host)
	}
	return c.JSON(http.StatusOK, CrawlRequestResponse{Message: "ok"})
}

func (cs *collectionServer) crawlThread(hostIn string) {
	host := hostOrUrlToUrl(hostIn)
	if host != hostIn {
		cs.log.Info("going to crawl", "in", hostIn, "as", host)
	}
	httpClient := http.Client{}
	rpcClient := xrpc.Client{
		Host:   host,
		Client: &httpClient,
	}
	if cs.ratelimitHeader != "" {
		rpcClient.Headers = map[string]string{
			"x-ratelimit-bypass": cs.ratelimitHeader,
		}
	}
	crawler := Crawler{
		Ctx:       cs.ctx,
		RpcClient: &rpcClient,
		QPS:       cs.PerPDSCrawlQPS,
		Results:   cs.ingestCrawl,
		Log:       cs.log,
	}
	start := time.Now()
	ok, crawlStats := cs.recordCrawlStart(host, start)
	if !ok {
		cs.log.Info("not crawling dup", "host", host)
		return
	}
	crawler.Stats = crawlStats
	cs.log.Info("crawling", "host", host)
	err := crawler.CrawlPDSRepoCollections()
	cs.clearActiveCrawl(host)
	pdsCrawledCounter.Inc()
	if err != nil {
		cs.log.Warn("crawl err", "host", host, "err", err)
	} else {
		dt := time.Since(start)
		cs.log.Info("crawl done", "host", host, "dt", dt)
	}
}

// recordCrawlStart returns true if ok, false if duplicate
func (cs *collectionServer) recordCrawlStart(host string, start time.Time) (ok bool, stats *CrawlStats) {
	cs.activeCrawlsLock.Lock()
	defer cs.activeCrawlsLock.Unlock()
	if cs.activeCrawls == nil {
		cs.activeCrawls = make(map[string]activeCrawl)
	} else {
		_, dup := cs.activeCrawls[host]
		if dup {
			return false, nil
		}
	}
	stats = new(CrawlStats)
	cs.activeCrawls[host] = activeCrawl{
		start: start,
		stats: stats,
	}
	return true, stats
}

func (cs *collectionServer) clearActiveCrawl(host string) {
	cs.activeCrawlsLock.Lock()
	defer cs.activeCrawlsLock.Unlock()
	if cs.activeCrawls == nil {
		return
	}
	delete(cs.activeCrawls, host)
}

type CrawlStatusResponse struct {
	HostCrawls map[string]HostCrawl `json:"host_starts"`
	ServerTime string               `json:"server_time"`
}
type HostCrawl struct {
	Start          string `json:"start"`
	ReposDescribed uint32 `json:"seen"`
}

// GET /v1/crawlStatus
func (cs *collectionServer) crawlStatus(c echo.Context) error {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader != cs.ExepctedAuthHeader {
		return c.JSON(http.StatusForbidden, CrawlRequestResponse{Error: "nope"})
	}
	var out CrawlStatusResponse
	out.HostCrawls = make(map[string]HostCrawl)
	cs.activeCrawlsLock.Lock()
	defer cs.activeCrawlsLock.Unlock()
	for host, rec := range cs.activeCrawls {
		start := rec.start
		out.HostCrawls[host] = HostCrawl{
			Start:          start.UTC().Format(time.RFC3339Nano),
			ReposDescribed: rec.stats.ReposDescribed.Load(),
		}
	}
	out.ServerTime = time.Now().UTC().Format(time.RFC3339Nano)
	return c.JSON(http.StatusOK, out)
}

func (cs *collectionServer) healthz(c echo.Context) error {
	// TODO: check database or upstream health?
	return c.String(http.StatusOK, "ok")
}

func loadBadwords(path string) (*BadwordsRE, error) {
	fin, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: could not open badwords, %w", path, err)
	}
	dec := json.NewDecoder(fin)
	var rules map[string][]string
	err = dec.Decode(&rules)
	if err != nil {
		return nil, fmt.Errorf("%s: badwords json, %w", path, err)
	}

	// compile a regex to search a string for any instance of a bad word, because we're expecting things runpooptogether
	badwords := rules["worst-words"]
	rwords := make([]string, len(badwords))
	for i, word := range badwords {
		rwords[i] = regexp.QuoteMeta(word)
	}
	reStr := strings.Join(rwords, "|")
	re, err := regexp.Compile(reStr)
	if err != nil {
		return nil, fmt.Errorf("%s: badwords regex, %w", path, err)
	}
	return &BadwordsRE{re: re}, nil
}

type BadwordsRE struct {
	re *regexp.Regexp
}

func (bw *BadwordsRE) HasBadword(s string) bool {
	// TODO: if this is too slow, try more specialized algorithm e.g. https://en.wikipedia.org/wiki/Aho%E2%80%93Corasick_algorithm
	return bw.re.FindString(s) != ""
}
