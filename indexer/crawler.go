package indexer

import (
	"context"
	"fmt"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"

	"go.opentelemetry.io/otel"
)

type CrawlDispatcher struct {
	ingest chan *models.ActorInfo

	repoSync chan *crawlWork

	catchup chan *catchupJob

	complete chan util.Uid

	maplk      sync.Mutex
	todo       map[util.Uid]*crawlWork
	inProgress map[util.Uid]*crawlWork

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
		complete:    make(chan util.Uid),
		catchup:     make(chan *catchupJob),
		doRepoCrawl: repoFn,
		concurrency: concurrency,
		todo:        make(map[util.Uid]*crawlWork),
		inProgress:  make(map[util.Uid]*crawlWork),
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
	var next *crawlWork
	var buffer []*crawlWork

	var rs chan *crawlWork
	for {
		select {
		case act := <-c.ingest:
			// TODO: max buffer size

			c.maplk.Lock()
			_, ok := c.inProgress[act.Uid]
			if ok {
				c.maplk.Unlock()
				break
			}

			_, has := c.todo[act.Uid]
			if has {
				c.maplk.Unlock()
				break
			}

			cw := &crawlWork{
				act:        act,
				initScrape: true,
			}
			c.todo[act.Uid] = cw
			c.maplk.Unlock()

			if next == nil {
				next = cw
				rs = c.repoSync
			} else {
				buffer = append(buffer, cw)
			}
		case rs <- next:
			c.maplk.Lock()
			delete(c.todo, next.act.Uid)
			c.inProgress[next.act.Uid] = next
			c.maplk.Unlock()

			if len(buffer) > 0 {
				next = buffer[0]
				buffer = buffer[1:]
			} else {
				next = nil
				rs = nil
			}
		case catchup := <-c.catchup:
			c.maplk.Lock()
			job, ok := c.todo[catchup.user.Uid]
			// TODO: in the event of receiving a rebase event, we *could* pre-empt all other pending events
			if ok {
				job.catchup = append(job.catchup, catchup)
				c.maplk.Unlock()
				break
			}

			job, ok = c.inProgress[catchup.user.Uid]
			if ok {
				job.next = append(job.next, catchup)
				c.maplk.Unlock()
				break
			}

			cw := &crawlWork{
				act:     catchup.user,
				catchup: []*catchupJob{catchup},
			}
			c.todo[catchup.user.Uid] = cw
			c.maplk.Unlock()

			if next == nil {
				next = cw
				rs = c.repoSync
			} else {
				buffer = append(buffer, cw)
			}

		case uid := <-c.complete:
			c.maplk.Lock()
			job, ok := c.inProgress[uid]
			if !ok {
				panic("should not be possible to not have a job in progress we receive a completion signal for")
			}
			delete(c.inProgress, uid)

			if len(job.next) > 0 {
				c.todo[uid] = job
				job.initScrape = false
				job.catchup = job.next
				job.next = nil
				if next == nil {
					next = job
					rs = c.repoSync
				} else {
					buffer = append(buffer, job)
				}
			}

			c.maplk.Unlock()

		}
	}
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
		panic("not today!")
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
		panic("not okay")
	}

	select {
	case c.catchup <- &catchupJob{
		evt:  evt,
		host: host,
		user: u,
	}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) RepoInSlowPath(ctx context.Context, host *models.PDS, uid util.Uid) bool {
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
