package indexer

import (
	"context"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"go.opentelemetry.io/otel"
)

type CrawlDispatcher struct {
	ingest chan *models.ActorInfo

	repoSync chan *crawlWork

	catchup chan *catchupJob

	complete chan uint

	doRepoCrawl func(context.Context, *crawlWork) error
}

func NewCrawlDispatcher(repoFn func(context.Context, *crawlWork) error) *CrawlDispatcher {
	return &CrawlDispatcher{
		ingest:      make(chan *models.ActorInfo),
		repoSync:    make(chan *crawlWork),
		complete:    make(chan uint),
		catchup:     make(chan *catchupJob),
		doRepoCrawl: repoFn,
	}
}

func (c *CrawlDispatcher) Run() {
	go c.mainLoop()

	for i := 0; i < 3; i++ {
		go c.fetchWorker()
	}
}

type catchupJob struct {
	evt  *events.RepoAppend
	host *models.PDS
	user *models.ActorInfo
}

type crawlWork struct {
	act        *models.ActorInfo
	initScrape bool

	catchup []*catchupJob

	// for events that come in while this actor is being processed
	next []*catchupJob
}

func (c *CrawlDispatcher) mainLoop() {
	var next *crawlWork
	var buffer []*crawlWork

	todo := make(map[uint]*crawlWork)
	inProgress := make(map[uint]*crawlWork)

	var rs chan *crawlWork
	for {
		select {
		case act := <-c.ingest:
			// TODO: max buffer size

			_, ok := inProgress[act.Uid]
			if ok {
				break
			}

			_, has := todo[act.Uid]
			if has {
				break
			}

			cw := &crawlWork{
				act:        act,
				initScrape: true,
			}
			todo[act.Uid] = cw

			if next == nil {
				next = cw
				rs = c.repoSync
			} else {
				buffer = append(buffer, cw)
			}
		case rs <- next:
			delete(todo, next.act.Uid)
			inProgress[next.act.Uid] = next

			if len(buffer) > 0 {
				next = buffer[0]
				buffer = buffer[1:]
			} else {
				next = nil
				rs = nil
			}
		case catchup := <-c.catchup:
			job, ok := todo[catchup.user.Uid]
			if ok {
				job.catchup = append(job.catchup, catchup)
				break
			}

			job, ok = inProgress[catchup.user.Uid]
			if ok {
				job.next = append(job.next, catchup)
				break
			}

			cw := &crawlWork{
				act:     catchup.user,
				catchup: []*catchupJob{catchup},
			}
			todo[catchup.user.Uid] = cw

			if next == nil {
				next = cw
				rs = c.repoSync
			} else {
				buffer = append(buffer, cw)
			}

		case uid := <-c.complete:
			job, ok := inProgress[uid]
			if !ok {
				panic("should not be possible to not have a job in progress we receive a completion signal for")
			}
			delete(inProgress, uid)

			if len(job.next) > 0 {
				todo[uid] = job
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
	ctx, span := otel.Tracer("crawler").Start(ctx, "addToCrawler")
	defer span.End()

	select {
	case c.ingest <- ai:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) AddToCatchupQueue(ctx context.Context, host *models.PDS, u *models.ActorInfo, evt *events.RepoAppend) error {
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
