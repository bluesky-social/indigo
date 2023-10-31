package bgs

import (
	"context"
	"fmt"
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
func (q *uniQueue) Pop() (*queueItem, bool) {
	q.lk.Lock()
	defer q.lk.Unlock()

	if len(q.q) == 0 {
		return nil, false
	}

	item := q.q[0]
	q.q = q.q[1:]
	delete(q.members, item.uid)

	compactionQueueDepth.Dec()
	return &item, true
}

type CompactorState struct {
	latestUID models.Uid
	latestDID string
	status    string
	stats     *carstore.CompactionStats
}

// Compactor is a compactor daemon that compacts repos in the background
type Compactor struct {
	q                 *uniQueue
	state             *CompactorState
	stateLk           sync.RWMutex
	exit              chan struct{}
	exited            chan struct{}
	requeueInterval   time.Duration
	requeueLimit      int
	requeueShardCount int
	requeueFast       bool
}

type CompactorOptions struct {
	RequeueInterval   time.Duration
	RequeueLimit      int
	RequeueShardCount int
	RequeueFast       bool
}

func DefaultCompactorOptions() *CompactorOptions {
	return &CompactorOptions{
		RequeueInterval:   time.Hour * 4,
		RequeueLimit:      0,
		RequeueShardCount: 50,
		RequeueFast:       true,
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
		state:             &CompactorState{},
		exit:              make(chan struct{}),
		exited:            make(chan struct{}),
		requeueInterval:   opts.RequeueInterval,
		requeueLimit:      opts.RequeueLimit,
		requeueFast:       opts.RequeueFast,
		requeueShardCount: opts.RequeueShardCount,
	}
}

type compactionStats struct {
	Completed map[models.Uid]*carstore.CompactionStats
	Targets   []carstore.CompactionTarget
}

func (c *Compactor) SetState(uid models.Uid, did, status string, stats *carstore.CompactionStats) {
	c.stateLk.Lock()
	defer c.stateLk.Unlock()

	c.state.latestUID = uid
	c.state.latestDID = did
	c.state.status = status
	c.state.stats = stats
}

func (c *Compactor) GetState() *CompactorState {
	c.stateLk.RLock()
	defer c.stateLk.RUnlock()

	return &CompactorState{
		latestUID: c.state.latestUID,
		latestDID: c.state.latestDID,
		status:    c.state.status,
		stats:     c.state.stats,
	}
}

var errNoReposToCompact = fmt.Errorf("no repos to compact")

// Start starts the compactor
func (c *Compactor) Start(bgs *BGS) {
	log.Info("starting compactor")
	go c.doWork(bgs)
	go func() {
		log.Infow("starting compactor requeue routine",
			"interval", c.requeueInterval,
			"limit", c.requeueLimit,
			"shardCount", c.requeueShardCount,
			"fast", c.requeueFast,
		)

		// Enqueue all repos on startup
		ctx := context.Background()
		ctx, span := otel.Tracer("compactor").Start(ctx, "RequeueRoutine")
		if err := c.EnqueueAllRepos(ctx, bgs, c.requeueLimit, c.requeueShardCount, c.requeueFast); err != nil {
			log.Errorw("failed to enqueue all repos", "err", err)
		}
		span.End()

		t := time.NewTicker(c.requeueInterval)
		for {
			select {
			case <-c.exit:
				return
			case <-t.C:
				ctx := context.Background()
				ctx, span := otel.Tracer("compactor").Start(ctx, "RequeueRoutine")
				if err := c.EnqueueAllRepos(ctx, bgs, c.requeueLimit, c.requeueShardCount, c.requeueFast); err != nil {
					log.Errorw("failed to enqueue all repos", "err", err)
				}
				span.End()
			}
		}
	}()
}

// Shutdown shuts down the compactor
func (c *Compactor) Shutdown() {
	log.Info("stopping compactor")
	close(c.exit)
	<-c.exited
	log.Info("compactor stopped")
}

func (c *Compactor) doWork(bgs *BGS) {
	for {
		select {
		case <-c.exit:
			log.Info("compactor worker exiting, no more active compactions running")
			close(c.exited)
			return
		default:
		}

		ctx := context.Background()
		start := time.Now()
		state, err := c.compactNext(ctx, bgs)
		if err != nil {
			if err == errNoReposToCompact {
				log.Debug("no repos to compact, waiting and retrying")
				time.Sleep(time.Second * 5)
				continue
			}
			log.Errorw("failed to compact repo",
				"err", err,
				"duration", time.Since(start),
			)
			// Pause for a bit to avoid spamming failed compactions
			time.Sleep(time.Millisecond * 100)
		} else {
			log.Infow("compacted repo",
				"uid", state.latestUID,
				"repo", state.latestDID,
				"status", state.status,
				"stats", state.stats,
				"duration", time.Since(start),
			)
		}
	}
}

func (c *Compactor) compactNext(ctx context.Context, bgs *BGS) (*CompactorState, error) {
	ctx, span := otel.Tracer("compactor").Start(ctx, "CompactNext")
	defer span.End()

	item, ok := c.q.Pop()
	if !ok || item == nil {
		return nil, errNoReposToCompact
	}

	c.SetState(item.uid, "unknown", "getting_user", nil)

	user, err := bgs.lookupUserByUID(ctx, item.uid)
	if err != nil {
		span.RecordError(err)
		c.SetState(item.uid, "unknown", "failed_getting_user", nil)
		return nil, fmt.Errorf("failed to get user %d: %w", item.uid, err)
	}

	span.SetAttributes(attribute.String("repo", user.Did), attribute.Int("uid", int(item.uid)))

	c.SetState(item.uid, user.Did, "compacting", nil)

	start := time.Now()
	st, err := bgs.repoman.CarStore().CompactUserShards(ctx, item.uid, item.fast)
	if err != nil {
		span.RecordError(err)
		c.SetState(item.uid, user.Did, "failed_compacting", nil)
		return nil, fmt.Errorf("failed to compact shards for user %d: %w", item.uid, err)
	}
	compactionDuration.Observe(time.Since(start).Seconds())

	span.SetAttributes(
		attribute.Int("shards.deleted", st.ShardsDeleted),
		attribute.Int("shards.new", st.NewShards),
		attribute.Int("dupes", st.DupeCount),
		attribute.Int("shards.skipped", st.SkippedShards),
		attribute.Int("refs", st.TotalRefs),
	)

	c.SetState(item.uid, user.Did, "done", st)

	return c.GetState(), nil
}

func (c *Compactor) EnqueueRepo(ctx context.Context, user User, fast bool) {
	ctx, span := otel.Tracer("compactor").Start(ctx, "EnqueueRepo")
	defer span.End()
	log.Infow("enqueueing compaction for repo", "repo", user.Did, "uid", user.ID, "fast", fast)
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

	log.Infow("done enqueueing all repos", "repos_enqueued", len(repos))

	return nil
}
