package indexer

import (
	"context"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/types"
)

type CrawlDispatcher struct {
	ingest chan *types.ActorInfo

	repoSync chan *types.ActorInfo

	doRepoCrawl func(context.Context, *types.ActorInfo) error
}

func NewCrawlDispatcher(repoFn func(context.Context, *types.ActorInfo) error) *CrawlDispatcher {
	return &CrawlDispatcher{
		ingest:      make(chan *types.ActorInfo),
		repoSync:    make(chan *types.ActorInfo),
		doRepoCrawl: repoFn,
	}
}

func (c *CrawlDispatcher) Run() {
	go c.mainLoop()

	for i := 0; i < 3; i++ {
		go c.fetchWorker()
	}
}

func (c *CrawlDispatcher) mainLoop() {
	var next *types.ActorInfo
	var buffer []*types.ActorInfo

	set := make(map[uint]*types.ActorInfo)
	//progress := make(map[uint]*types.ActorInfo)

	var rs chan *types.ActorInfo
	for {
		select {
		case act := <-c.ingest:
			// TODO: max buffer size

			_, has := set[act.Uid]
			if has {
				break
			}
			set[act.Uid] = act

			if next == nil {
				next = act
				rs = c.repoSync
			} else {
				buffer = append(buffer, act)
			}
		case rs <- next:
			delete(set, next.Uid)

			if len(buffer) > 0 {
				next = buffer[0]
				buffer = buffer[1:]
			} else {
				next = nil
				rs = nil
			}
		}
	}
}

func (c *CrawlDispatcher) fetchWorker() {
	for {
		select {
		case job := <-c.repoSync:
			if err := c.doRepoCrawl(context.TODO(), job); err != nil {
				log.Errorf("failed to perform repo crawl of %q: %s", job, err)
			}
		}
	}
}

func (c *CrawlDispatcher) Crawl(ctx context.Context, ai *types.ActorInfo) error {
	select {
	case c.ingest <- ai:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CrawlDispatcher) AddToCatchupQueue(ctx context.Context, host *types.PDS, u uint, evt *events.RepoEvent) error {
	return nil
}
