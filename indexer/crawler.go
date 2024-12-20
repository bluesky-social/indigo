package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"
)

type CrawlDispatcher struct {
	// from Crawl()
	ingest chan *crawlWork

	// from AddToCatchupQueue()
	catchup chan *crawlWork

	// from mainLoop to fetchWorker()
	repoSync chan *crawlWork

	// from fetchWorker back to mainLoop
	complete chan models.Uid

	// maplk is around both todo and inProgress
	maplk      sync.Mutex
	todo       map[models.Uid]*crawlWork
	inProgress map[models.Uid]*crawlWork

	repoFetcher CrawlRepoFetcher

	concurrency int

	log *slog.Logger

	done chan struct{}
}

// this is what we need of RepoFetcher
type CrawlRepoFetcher interface {
	FetchAndIndexRepo(ctx context.Context, job *crawlWork) error
}

func NewCrawlDispatcher(repoFetcher CrawlRepoFetcher, concurrency int, log *slog.Logger) (*CrawlDispatcher, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("must specify a non-zero positive integer for crawl dispatcher concurrency")
	}

	out := &CrawlDispatcher{
		ingest:      make(chan *crawlWork),
		repoSync:    make(chan *crawlWork, concurrency*2),
		complete:    make(chan models.Uid, concurrency*2),
		catchup:     make(chan *crawlWork),
		repoFetcher: repoFetcher,
		concurrency: concurrency,
		todo:        make(map[models.Uid]*crawlWork),
		inProgress:  make(map[models.Uid]*crawlWork),
		log:         log,
		done:        make(chan struct{}),
	}
	go out.CatchupRepoGaugePoller()

	return out, nil
}

func (c *CrawlDispatcher) Run() {
	go c.mainLoop()

	for i := 0; i < c.concurrency; i++ {
		go c.fetchWorker()
	}
}

func (c *CrawlDispatcher) Shutdown() {
	close(c.done)
}

type catchupJob struct {
	evt  *comatproto.SyncSubscribeRepos_Commit
	host *models.PDS
	user *models.ActorInfo
}

type crawlWork struct {
	act        *models.ActorInfo
	initScrape bool

	// for events that come in while this actor's crawl is enqueued
	// catchup items are processed during the crawl
	catchup []*catchupJob

	// for events that come in while this actor is being processed
	// next items are processed after the crawl
	next []*catchupJob
}

func (c *CrawlDispatcher) mainLoop() {
	for {
		var crawlJob *crawlWork = nil
		select {
		case crawlJob = <-c.ingest:
			c.log.Info("ml ingest", "pds", crawlJob.act.PDS, "uid", crawlJob.act.Uid)
		case crawlJob = <-c.catchup:
			c.log.Info("ml catchup", "pds", crawlJob.act.PDS, "uid", crawlJob.act.Uid)
			// from AddToCatchupQueue()
			// CatchupJobs are for processing events that come in while a crawl is in progress
			// They are lower priority than new crawls so we only add them to the queue if there isn't already a job in progress
		case uid := <-c.complete:
			crawlJob = c.recordComplete(uid)
		}

		if crawlJob != nil {
			c.repoSync <- crawlJob
			c.dequeueJob(crawlJob)
		}
	}
}

func (c *CrawlDispatcher) recordComplete(uid models.Uid) *crawlWork {
	c.maplk.Lock()
	defer c.maplk.Unlock()

	job, ok := c.inProgress[uid]
	if !ok {
		panic("should not be possible to not have a job in progress we receive a completion signal for")
	}
	delete(c.inProgress, uid)
	c.log.Info("ml complete", "pds", job.act.PDS, "uid", job.act.Uid)

	// If there are any subsequent jobs for this UID, add it back to the todo list or buffer.
	// We're basically pumping the `next` queue into the `catchup` queue and will do this over and over until the `next` queue is empty.
	if len(job.next) > 0 {
		c.todo[uid] = job
		job.initScrape = false
		job.catchup = job.next
		job.next = nil
		return job
	}
	return nil
}

// enqueueJobForActor adds a new crawl job to the todo list if there isn't already a job in progress for this actor
func (c *CrawlDispatcher) enqueueJobForActor(ai *models.ActorInfo) *crawlWork {
	c.maplk.Lock()
	defer c.maplk.Unlock()
	_, ok := c.inProgress[ai.Uid]
	if ok {
		return nil
	}

	_, has := c.todo[ai.Uid]
	if has {
		return nil
	}

	crawlJob := &crawlWork{
		act:        ai,
		initScrape: true,
	}
	c.todo[ai.Uid] = crawlJob
	return crawlJob
}

// dequeueJob removes a job from the todo list and adds it to the inProgress list
func (c *CrawlDispatcher) dequeueJob(job *crawlWork) {
	c.maplk.Lock()
	defer c.maplk.Unlock()
	delete(c.todo, job.act.Uid)
	c.inProgress[job.act.Uid] = job
}

func (c *CrawlDispatcher) addToCatchupQueue(catchup *catchupJob) *crawlWork {
	c.maplk.Lock()
	defer c.maplk.Unlock()

	// If the actor crawl is enqueued, we can append to the catchup queue which gets emptied during the crawl
	job, ok := c.todo[catchup.user.Uid]
	if ok {
		catchupEventsEnqueued.WithLabelValues("todo").Inc()
		job.catchup = append(job.catchup, catchup)
		return nil
	}

	// If the actor crawl is in progress, we can append to the nextr queue which gets emptied after the crawl
	job, ok = c.inProgress[catchup.user.Uid]
	if ok {
		catchupEventsEnqueued.WithLabelValues("prog").Inc()
		job.next = append(job.next, catchup)
		return nil
	}

	catchupEventsEnqueued.WithLabelValues("new").Inc()
	// Otherwise, we need to create a new crawl job for this actor and enqueue it
	cw := &crawlWork{
		act:     catchup.user,
		catchup: []*catchupJob{catchup},
	}
	c.todo[catchup.user.Uid] = cw
	return cw
}

func (c *CrawlDispatcher) fetchWorker() {
	for {
		select {
		case job := <-c.repoSync:
			c.log.Info("fetchWorker start", "pds", job.act.PDS, "uid", job.act.Uid)
			// TODO: only run one fetchWorker per PDS because FetchAndIndexRepo will Limiter.Wait() to ensure rate limits per-PDS
			if err := c.repoFetcher.FetchAndIndexRepo(context.TODO(), job); err != nil {
				c.log.Error("failed to perform repo crawl", "did", job.act.Did, "err", err)
			} else {
				c.log.Info("fetchWorker done", "pds", job.act.PDS, "uid", job.act.Uid)
			}

			// TODO: do we still just do this if it errors?
			c.complete <- job.act.Uid
		}
	}
}

func (c *CrawlDispatcher) Crawl(ctx context.Context, ai *models.ActorInfo) error {
	if ai.PDS == 0 {
		panic("must have pds for user in queue")
	}

	cw := c.enqueueJobForActor(ai)
	if cw == nil {
		return nil
	}

	userCrawlsEnqueued.Inc()

	//ctx, span := otel.Tracer("crawler").Start(ctx, "addToCrawler")
	//defer span.End()

	c.log.Info("crawl", "pds", cw.act.PDS, "uid", cw.act.Uid)
	select {
	case c.ingest <- cw:
		c.log.Info("crawl posted", "pds", cw.act.PDS, "uid", cw.act.Uid)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) AddToCatchupQueue(ctx context.Context, host *models.PDS, u *models.ActorInfo, evt *comatproto.SyncSubscribeRepos_Commit) error {
	if u.PDS == 0 {
		panic("must have pds for user in queue")
	}

	catchup := &catchupJob{
		evt:  evt,
		host: host,
		user: u,
	}

	cw := c.addToCatchupQueue(catchup)
	if cw == nil {
		return nil
	}

	c.log.Info("catchup", "pds", cw.act.PDS, "uid", cw.act.Uid)
	select {
	case c.catchup <- cw:
		c.log.Info("catchup posted", "pds", cw.act.PDS, "uid", cw.act.Uid)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) RepoInSlowPath(ctx context.Context, uid models.Uid) bool {
	c.maplk.Lock()
	defer c.maplk.Unlock()
	if _, ok := c.todo[uid]; ok {
		return true
	}

	if _, ok := c.inProgress[uid]; ok {
		return true
	}

	return false
}

func (c *CrawlDispatcher) countReposInSlowPath() int {
	c.maplk.Lock()
	defer c.maplk.Unlock()
	return len(c.inProgress) + len(c.todo)
}

func (c *CrawlDispatcher) CatchupRepoGaugePoller() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
		case <-ticker.C:
			catchupReposGauge.Set(float64(c.countReposInSlowPath()))
		}
	}
}
