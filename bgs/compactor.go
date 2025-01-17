package bgs

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type queueItem struct {
	uid  models.Uid
	fast bool
}

// uniQueue is a queue that only allows one instance of a given uid
type uniQueue struct {
	q       []queueItem
	members map[models.Uid]struct{}
	lk      sync.Mutex
}

// Append appends a uid to the end of the queue if it doesn't already exist
func (q *uniQueue) Append(uid models.Uid, fast bool) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if _, ok := q.members[uid]; ok {
		return
	}

	q.q = append(q.q, queueItem{uid: uid, fast: fast})
	q.members[uid] = struct{}{}
	compactionQueueDepth.Inc()
}

// Prepend prepends a uid to the beginning of the queue if it doesn't already exist
func (q *uniQueue) Prepend(uid models.Uid, fast bool) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if _, ok := q.members[uid]; ok {
		return
	}

	q.q = append([]queueItem{{uid: uid, fast: fast}}, q.q...)
	q.members[uid] = struct{}{}
	compactionQueueDepth.Inc()
}

// Has returns true if the queue contains the given uid
func (q *uniQueue) Has(uid models.Uid) bool {
	q.lk.Lock()
	defer q.lk.Unlock()

	_, ok := q.members[uid]
	return ok
}

// Remove removes the given uid from the queue
func (q *uniQueue) Remove(uid models.Uid) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if _, ok := q.members[uid]; !ok {
		return
	}

	for i, item := range q.q {
		if item.uid == uid {
			q.q = append(q.q[:i], q.q[i+1:]...)
			break
		}
	}

	delete(q.members, uid)
	compactionQueueDepth.Dec()
}

// Pop pops the first item off the front of the queue
func (q *uniQueue) Pop() (queueItem, bool) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if len(q.q) == 0 {
		return queueItem{}, false
	}

	item := q.q[0]
	q.q = q.q[1:]
	delete(q.members, item.uid)

	compactionQueueDepth.Dec()
	return item, true
}

// PopRandom pops a random item off the of the queue
// Note: this disrupts the sorted order of the queue and in-order is no longer quite in-order. The randomly popped element is replaced with the last element.
func (q *uniQueue) PopRandom() (queueItem, bool) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if len(q.q) == 0 {
		return queueItem{}, false
	}

	var item queueItem
	if len(q.q) == 1 {
		item = q.q[0]
		q.q = nil
	} else {
		pos := rand.IntN(len(q.q))
		item = q.q[pos]
		last := len(q.q) - 1
		q.q[pos] = q.q[last]
		q.q = q.q[:last]
	}
	delete(q.members, item.uid)

	compactionQueueDepth.Dec()
	return item, true
}

type CompactorState struct {
	latestUID models.Uid
	latestDID string
	status    string
	stats     *carstore.CompactionStats
}

func (cstate *CompactorState) set(uid models.Uid, did, status string, stats *carstore.CompactionStats) {
	cstate.latestUID = uid
	cstate.latestDID = did
	cstate.status = status
	cstate.stats = stats
}

// Compactor is a compactor daemon that compacts repos in the background
type Compactor struct {
	q                 *uniQueue
	stateLk           sync.RWMutex
	exit              chan struct{}
	requeueInterval   time.Duration
	requeueLimit      int
	requeueShardCount int
	requeueFast       bool

	numWorkers int
	wg         sync.WaitGroup
}

type CompactorOptions struct {
	RequeueInterval   time.Duration
	RequeueLimit      int
	RequeueShardCount int
	RequeueFast       bool
	NumWorkers        int
}

func DefaultCompactorOptions() *CompactorOptions {
	return &CompactorOptions{
		RequeueInterval:   time.Hour * 4,
		RequeueLimit:      0,
		RequeueShardCount: 50,
		RequeueFast:       true,
		NumWorkers:        2,
	}
}

func NewCompactor(opts *CompactorOptions) *Compactor {
	if opts == nil {
		opts = DefaultCompactorOptions()
	}

	return &Compactor{
		q: &uniQueue{
			members: make(map[models.Uid]struct{}),
		},
		exit:              make(chan struct{}),
		requeueInterval:   opts.RequeueInterval,
		requeueLimit:      opts.RequeueLimit,
		requeueFast:       opts.RequeueFast,
		requeueShardCount: opts.RequeueShardCount,
		numWorkers:        opts.NumWorkers,
	}
}

type compactionStats struct {
	Completed map[models.Uid]*carstore.CompactionStats
	Targets   []carstore.CompactionTarget
}

var errNoReposToCompact = fmt.Errorf("no repos to compact")

// Start starts the compactor
func (c *Compactor) Start(bgs *BGS) {
	log.Info("starting compactor")
	c.wg.Add(c.numWorkers)
	for i := range c.numWorkers {
		strategy := NextInOrder
		if i%2 != 0 {
			strategy = NextRandom
		}
		go c.doWork(bgs, strategy)
	}
	if c.requeueInterval > 0 {
		go func() {
			log.Info("starting compactor requeue routine",
				"interval", c.requeueInterval,
				"limit", c.requeueLimit,
				"shardCount", c.requeueShardCount,
				"fast", c.requeueFast,
			)

			t := time.NewTicker(c.requeueInterval)
			for {
				select {
				case <-c.exit:
					return
				case <-t.C:
					ctx := context.Background()
					ctx, span := otel.Tracer("compactor").Start(ctx, "RequeueRoutine")
					if err := c.EnqueueAllRepos(ctx, bgs, c.requeueLimit, c.requeueShardCount, c.requeueFast); err != nil {
						log.Error("failed to enqueue all repos", "err", err)
					}
					span.End()
				}
			}
		}()
	}
}

// Shutdown shuts down the compactor
func (c *Compactor) Shutdown() {
	log.Info("stopping compactor")
	close(c.exit)
	c.wg.Wait()
	log.Info("compactor stopped")
}

func (c *Compactor) doWork(bgs *BGS, strategy NextStrategy) {
	defer c.wg.Done()
	for {
		select {
		case <-c.exit:
			log.Info("compactor worker exiting, no more active compactions running")
			return
		default:
		}

		ctx := context.Background()
		start := time.Now()
		state, err := c.compactNext(ctx, bgs, strategy)
		if err != nil {
			if err == errNoReposToCompact {
				log.Debug("no repos to compact, waiting and retrying")
				time.Sleep(time.Second * 5)
				continue
			}
			log.Error("failed to compact repo",
				"err", err,
				"uid", state.latestUID,
				"repo", state.latestDID,
				"status", state.status,
				"stats", state.stats,
				"duration", time.Since(start),
			)
			// Pause for a bit to avoid spamming failed compactions
			time.Sleep(time.Millisecond * 100)
		} else {
			log.Info("compacted repo",
				"uid", state.latestUID,
				"repo", state.latestDID,
				"status", state.status,
				"stats", state.stats,
				"duration", time.Since(start),
			)
		}
	}
}

type NextStrategy int

const (
	NextInOrder NextStrategy = iota
	NextRandom
)

func (c *Compactor) compactNext(ctx context.Context, bgs *BGS, strategy NextStrategy) (CompactorState, error) {
	ctx, span := otel.Tracer("compactor").Start(ctx, "CompactNext")
	defer span.End()

	var item queueItem
	var ok bool
	switch strategy {
	case NextRandom:
		item, ok = c.q.PopRandom()
	default:
		item, ok = c.q.Pop()
	}
	if !ok {
		return CompactorState{}, errNoReposToCompact
	}

	state := CompactorState{
		latestUID: item.uid,
		latestDID: "unknown",
		status:    "getting_user",
	}

	user, err := bgs.lookupUserByUID(ctx, item.uid)
	if err != nil {
		span.RecordError(err)
		state.status = "failed_getting_user"
		err := fmt.Errorf("failed to get user %d: %w", item.uid, err)
		return state, err
	}

	span.SetAttributes(attribute.String("repo", user.Did), attribute.Int("uid", int(item.uid)))

	state.latestDID = user.Did

	start := time.Now()
	st, err := bgs.repoman.CarStore().CompactUserShards(ctx, item.uid, item.fast)
	if err != nil {
		span.RecordError(err)
		state.status = "failed_compacting"
		err := fmt.Errorf("failed to compact shards for user %d: %w", item.uid, err)
		return state, err
	}
	compactionDuration.Observe(time.Since(start).Seconds())

	span.SetAttributes(
		attribute.Int("shards.deleted", st.ShardsDeleted),
		attribute.Int("shards.new", st.NewShards),
		attribute.Int("dupes", st.DupeCount),
		attribute.Int("shards.skipped", st.SkippedShards),
		attribute.Int("refs", st.TotalRefs),
	)

	state.status = "done"
	state.stats = st

	return state, nil
}

func (c *Compactor) EnqueueRepo(ctx context.Context, user *User, fast bool) {
	ctx, span := otel.Tracer("compactor").Start(ctx, "EnqueueRepo")
	defer span.End()
	log.Info("enqueueing compaction for repo", "repo", user.Did, "uid", user.ID, "fast", fast)
	c.q.Append(user.ID, fast)
}

// EnqueueAllRepos enqueues all repos for compaction
// lim is the maximum number of repos to enqueue
// shardCount is the number of shards to compact per user (0 = default of 50)
// fast is whether to use the fast compaction method (skip large shards)
func (c *Compactor) EnqueueAllRepos(ctx context.Context, bgs *BGS, lim int, shardCount int, fast bool) error {
	ctx, span := otel.Tracer("compactor").Start(ctx, "EnqueueAllRepos")
	defer span.End()

	span.SetAttributes(
		attribute.Int("lim", lim),
		attribute.Int("shardCount", shardCount),
		attribute.Bool("fast", fast),
	)

	if shardCount == 0 {
		shardCount = 20
	}

	span.SetAttributes(attribute.Int("clampedShardCount", shardCount))

	log := log.With("source", "compactor_enqueue_all_repos", "lim", lim, "shardCount", shardCount, "fast", fast)
	log.Info("enqueueing all repos")

	repos, err := bgs.repoman.CarStore().GetCompactionTargets(ctx, shardCount)
	if err != nil {
		return fmt.Errorf("failed to get repos to compact: %w", err)
	}

	span.SetAttributes(attribute.Int("repos", len(repos)))

	if lim > 0 && len(repos) > lim {
		repos = repos[:lim]
	}

	span.SetAttributes(attribute.Int("clampedRepos", len(repos)))

	for _, r := range repos {
		c.q.Append(r.Usr, fast)
	}

	log.Info("done enqueueing all repos", "repos_enqueued", len(repos))

	return nil
}
