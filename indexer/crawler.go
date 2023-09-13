package indexer

import (
	"context"
	"fmt"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"

	"go.opentelemetry.io/otel"
)

type CrawlDispatcher struct {
	ingest chan *models.ActorInfo

	repoSync chan *crawlWork

	catchup chan *crawlWork

	complete chan models.Uid

	maplk      sync.Mutex
	todo       map[models.Uid]*crawlWork
	inProgress map[models.Uid]*crawlWork

	doRepoCrawl func(context.Context, *crawlWork) error

	concurrency int
}

func NewCrawlDispatcher(repoFn func(context.Context, *crawlWork) error, concurrency int) (*CrawlDispatcher, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("must specify a non-zero positive integer for crawl dispatcher concurrency")
	}

	return &CrawlDispatcher{
		ingest:      make(chan *models.ActorInfo),
		repoSync:    make(chan *crawlWork),
		complete:    make(chan models.Uid),
		catchup:     make(chan *crawlWork),
		doRepoCrawl: repoFn,
		concurrency: concurrency,
		todo:        make(map[models.Uid]*crawlWork),
		inProgress:  make(map[models.Uid]*crawlWork),
	}, nil
}

func (c *CrawlDispatcher) Run() {
	go c.mainLoop()

	for i := 0; i < c.concurrency; i++ {
		go c.fetchWorker()
	}
}

type catchupJob struct {
	evt  *comatproto.SyncSubscribeRepos_Commit
	host *models.PDS
	user *models.ActorInfo
}

type crawlWork struct {
	act        *models.ActorInfo
	initScrape bool

	catchup []*catchupJob

	// for events that come in while this actor is being processed
	next []*catchupJob

	rebase *catchupJob
}

func (c *CrawlDispatcher) mainLoop() {
	var nextDispatchedJob *crawlWork
	var jobsAwaitingDispatch []*crawlWork

	// dispatchQueue represents the repoSync channel to dispatch crawl work.
	var dispatchQueue chan *crawlWork

	for {
		select {
		case actorToCrawl := <-c.ingest:
			// TODO: max buffer size
			c.maplk.Lock()

			_, ok := c.inProgress[actorToCrawl.Uid]
			if ok {
				c.maplk.Unlock()
				break
			}

			_, has := c.todo[actorToCrawl.Uid]
			if has {
				c.maplk.Unlock()
				break
			}

			crawlJob := &crawlWork{
				act:        actorToCrawl,
				initScrape: true,
			}
			c.todo[actorToCrawl.Uid] = crawlJob
			c.maplk.Unlock()

			if nextDispatchedJob == nil {
				nextDispatchedJob = crawlJob
				dispatchQueue = c.repoSync
			} else {
				jobsAwaitingDispatch = append(jobsAwaitingDispatch, crawlJob)
			}
		case dispatchQueue <- nextDispatchedJob:
			c.maplk.Lock()

			delete(c.todo, nextDispatchedJob.act.Uid)
			c.inProgress[nextDispatchedJob.act.Uid] = nextDispatchedJob
			c.maplk.Unlock()

			if len(jobsAwaitingDispatch) > 0 {
				nextDispatchedJob = jobsAwaitingDispatch[0]
				jobsAwaitingDispatch = jobsAwaitingDispatch[1:]
			} else {
				nextDispatchedJob = nil
				dispatchQueue = nil
			}
		case lowPriorityJob := <-c.catchup:
			if nextDispatchedJob == nil {
				nextDispatchedJob = lowPriorityJob
				dispatchQueue = c.repoSync
			} else {
				jobsAwaitingDispatch = append(jobsAwaitingDispatch, lowPriorityJob)
			}
		case uid := <-c.complete:
			c.maplk.Lock()

			job, ok := c.inProgress[uid]
			if !ok {
				panic("should not be possible to not have a job in progress we receive a completion signal for")
			}
			delete(c.inProgress, uid)

			// If there are any subsequent jobs for this UID, add it back to the todo list or buffer.
			// We're basically pumping the `next` queue into the `catchup` queue and will do this over and over until the `next` queue is empty.
			if len(job.next) > 0 {
				c.todo[uid] = job
				job.initScrape = false
				job.catchup = job.next
				job.next = nil
				if nextDispatchedJob == nil {
					nextDispatchedJob = job
					dispatchQueue = c.repoSync
				} else {
					jobsAwaitingDispatch = append(jobsAwaitingDispatch, job)
				}
			}
			c.maplk.Unlock()
		}
	}
}

func (c *CrawlDispatcher) addToCatchupQueue(catchup *catchupJob) *crawlWork {
	catchupEventsEnqueued.Inc()
	c.maplk.Lock()
	defer c.maplk.Unlock()
	job, ok := c.todo[catchup.user.Uid]
	// TODO: in the event of receiving a rebase event, we *could* pre-empt all other pending events
	if ok {
		job.catchup = append(job.catchup, catchup)
		return nil
	}

	job, ok = c.inProgress[catchup.user.Uid]
	if ok {
		job.next = append(job.next, catchup)
		return nil
	}

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
			if err := c.doRepoCrawl(context.TODO(), job); err != nil {
				log.Errorf("failed to perform repo crawl of %q: %s", job.act.Did, err)
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

	ctx, span := otel.Tracer("crawler").Start(ctx, "addToCrawler")
	defer span.End()

	select {
	case c.ingest <- ai:
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

	select {
	case c.catchup <- cw:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) RepoInSlowPath(ctx context.Context, host *models.PDS, uid models.Uid) bool {
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
