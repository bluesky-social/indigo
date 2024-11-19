package indexer

import (
	"container/heap"
	"context"
	"fmt"
	"golang.org/x/time/rate"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"

	"go.opentelemetry.io/otel"
)

type CrawlDispatcher struct {
	ingest chan *models.ActorInfo

	catchup chan *crawlWork

	complete chan models.Uid

	maplk      sync.Mutex
	todo       map[models.Uid]*crawlWork
	inProgress map[models.Uid]*crawlWork

	repoFetcher CrawlRepoFetcher

	concurrency int

	repoSyncHeap []*crawlWork
	// map [pdsID] *crawlWork pending jobs for that PDS, head of linked list on .nextInPds
	repoSyncPds  map[uint]*crawlWork
	repoSyncLock sync.Mutex
	repoSyncCond sync.Cond
}

// this is what we need of RepoFetcher, made interface so it can be passed in without dependency
type CrawlRepoFetcher interface {
	FetchAndIndexRepo(ctx context.Context, job *crawlWork) error
	GetOrCreateLimiterLazy(pdsID uint) *rate.Limiter
}

func NewCrawlDispatcher(repoFetcher CrawlRepoFetcher, concurrency int) (*CrawlDispatcher, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("must specify a non-zero positive integer for crawl dispatcher concurrency")
	}

	out := &CrawlDispatcher{
		ingest:      make(chan *models.ActorInfo),
		complete:    make(chan models.Uid),
		catchup:     make(chan *crawlWork),
		repoFetcher: repoFetcher,
		concurrency: concurrency,
		todo:        make(map[models.Uid]*crawlWork),
		inProgress:  make(map[models.Uid]*crawlWork),
		repoSyncPds: make(map[uint]*crawlWork),
	}
	out.repoSyncCond.L = &out.repoSyncLock
	return out, nil
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

	// for events that come in while this actor's crawl is enqueued
	// catchup items are processed during the crawl
	catchup []*catchupJob

	// for events that come in while this actor is being processed
	// next items are processed after the crawl
	next []*catchupJob

	eligibleTime    time.Time
	nextInPds       *crawlWork
	alreadyEnheaped bool
}

func (c *CrawlDispatcher) mainLoop() {
	for {
		var crawlJob *crawlWork = nil
		select {
		case actorToCrawl := <-c.ingest:
			// TODO: max buffer size
			crawlJob = c.enqueueJobForActor(actorToCrawl)
		case crawlJob = <-c.catchup:
			// CatchupJobs are for processing events that come in while a crawl is in progress
			// They are lower priority than new crawls so we only add them to the queue if there isn't already a job in progress
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
				crawlJob = job
			}
			c.maplk.Unlock()
		}
		if crawlJob != nil {
			pdsID := crawlJob.act.PDS
			limiter := c.repoFetcher.GetOrCreateLimiterLazy(pdsID)
			now := time.Now()
			wouldDelay := limiter.ReserveN(now, 1).DelayFrom(now)
			crawlJob.eligibleTime = now.Add(wouldDelay)
			// put crawl job on heap sorted by eligible time
			c.enheapJob(crawlJob)
			c.dequeueJob(crawlJob)
			crawlJob = nil
		}
	}
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
		job := c.nextJob()
		nextInPds := job.nextInPds
		job.nextInPds = nil
		if err := c.repoFetcher.FetchAndIndexRepo(context.TODO(), job); err != nil {
			log.Errorf("failed to perform repo crawl of %q: %s", job.act.Did, err)
		}

		// TODO: do we still just do this if it errors?
		c.complete <- job.act.Uid

		if nextInPds != nil {
			c.enheapJob(nextInPds)
		}
	}
}

func (c *CrawlDispatcher) Crawl(ctx context.Context, ai *models.ActorInfo) error {
	if ai.PDS == 0 {
		panic("must have pds for user in queue")
	}

	userCrawlsEnqueued.Inc()

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

// priority-queue for crawlJob based on eligibleTime
func (c *CrawlDispatcher) enheapJob(crawlJob *crawlWork) {
	if crawlJob.alreadyEnheaped {
		log.Errorf("CrawlDispatcher pds %d uid %d trying to enheap alreadyEnheaped", crawlJob.alreadyEnheaped, crawlJob.act.Uid)
	}
	c.repoSyncLock.Lock()
	defer c.repoSyncLock.Unlock()
	pdsJobs, has := c.repoSyncPds[crawlJob.act.PDS]
	if has {
		if !pdsJobs.alreadyEnheaped {
			heap.Push(c, crawlJob)
			c.repoSyncCond.Signal()
			pdsJobs.alreadyEnheaped = true
		}
		if pdsJobs == crawlJob {
			// this _should_ be true if nextInPds was already there when we called .nextJob()
			return
		}
		for pdsJobs.nextInPds != nil {
			pdsJobs = pdsJobs.nextInPds
			if pdsJobs == crawlJob {
				// we re-enheap something later? weird but okay?
				return
			}
		}
		pdsJobs.nextInPds = crawlJob
		return
	} else {
		c.repoSyncPds[crawlJob.act.PDS] = crawlJob
	}
	if !crawlJob.alreadyEnheaped {
		heap.Push(c, crawlJob)
		c.repoSyncCond.Signal()
		crawlJob.alreadyEnheaped = true
	}
}

// nextJob returns next available crawlJob based on eligibleTime; block until some available.
// The caller of .nextJob() should .enheapJob(crawlJob.nextInPds) if any after executing crawlJob's work
//
// There's a tiny race where .nextJob() could return the only work for a PDS,
// outside event could .enheapJob() a next one for that PDS,
// get enheaped as available immediately because the rate limiter hasn't ticked the work done from .nextJob() above,
// and then the worker trying to execute the next enheaped work for the PDS would execute immediately but Sleep() to wait for the rate limiter.
// We will call this 'not too bad', 'good enough for now'. -- bolson 2024-11
func (c *CrawlDispatcher) nextJob() *crawlWork {
	c.repoSyncLock.Lock()
	defer c.repoSyncLock.Unlock()
	//retry:
	for len(c.repoSyncHeap) == 0 {
		c.repoSyncCond.Wait()
	}
	x := heap.Pop(c)
	crawlJob := x.(*crawlWork)
	if crawlJob.nextInPds != nil {
		prev := c.repoSyncPds[crawlJob.act.PDS]
		if prev != crawlJob {
			log.Errorf("CrawlDispatcher internal: pds %d next is not next in eligible heap, dropping all PDS work", crawlJob.act.PDS)
			//delete(c.repoSyncPds, crawlJob.act.PDS)
			//goto retry
		}
		//c.repoSyncPds[crawlJob.act.PDS] = crawlJob.nextInPds
	} // else {
	delete(c.repoSyncPds, crawlJob.act.PDS)
	//}
	crawlJob.alreadyEnheaped = false
	return crawlJob
}

// part of container/heap.Interface and sort.Interface
// c.repoSyncLock MUST ALREADY BE HELD BEFORE HEAP OPERATIONS
func (c *CrawlDispatcher) Len() int {
	return len(c.repoSyncHeap)
}

// part of container/heap.Interface and sort.Interface
// c.repoSyncLock MUST ALREADY BE HELD BEFORE HEAP OPERATIONS
func (c *CrawlDispatcher) Less(i, j int) bool {
	return c.repoSyncHeap[i].eligibleTime.Before(c.repoSyncHeap[j].eligibleTime)
}

// part of container/heap.Interface and sort.Interface
// c.repoSyncLock MUST ALREADY BE HELD BEFORE HEAP OPERATIONS
func (c *CrawlDispatcher) Swap(i, j int) {
	t := c.repoSyncHeap[i]
	c.repoSyncHeap[i] = c.repoSyncHeap[j]
	c.repoSyncHeap[j] = t
}

// part of container/heap.Interface
// c.repoSyncLock MUST ALREADY BE HELD BEFORE HEAP OPERATIONS
func (c *CrawlDispatcher) Push(x any) {
	c.repoSyncHeap = append(c.repoSyncHeap, x.(*crawlWork))
}

// part of container/heap.Interface
// c.repoSyncLock MUST ALREADY BE HELD BEFORE HEAP OPERATIONS
func (c *CrawlDispatcher) Pop() any {
	heaplen := len(c.repoSyncHeap)
	out := c.repoSyncHeap[heaplen-1]
	c.repoSyncHeap = c.repoSyncHeap[:heaplen-1]
	return out
}
