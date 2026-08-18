package dbpersist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"
	arc "github.com/hashicorp/golang-lru/arc/v2"

	cid "github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

var log = slog.Default().With("system", "dbpersist")

type PersistenceBatchItem struct {
	Record *RepoEventRecord
	Event  *events.XRPCStreamEvent
}

type Options struct {
	MaxBatchSize         int
	MinBatchSize         int
	MaxTimeBetweenFlush  time.Duration
	CheckBatchInterval   time.Duration
	UIDCacheSize         int
	DIDCacheSize         int
	PlaybackBatchSize    int
	HydrationConcurrency int
}

func DefaultOptions() *Options {
	return &Options{
		MaxBatchSize:         200,
		MinBatchSize:         10,
		MaxTimeBetweenFlush:  500 * time.Millisecond,
		CheckBatchInterval:   100 * time.Millisecond,
		UIDCacheSize:         10000,
		DIDCacheSize:         10000,
		PlaybackBatchSize:    500,
		HydrationConcurrency: 10,
	}
}

type DbPersistence struct {
	db *gorm.DB

	cs carstore.CarStore

	lk sync.Mutex

	broadcast func(*events.XRPCStreamEvent)

	batch        []*PersistenceBatchItem
	batchOptions Options
	lastFlush    time.Time

	uidCache *arc.ARCCache[models.Uid, string]
	didCache *arc.ARCCache[string, models.Uid]
}

type RepoEventRecord struct {
	Seq       uint `gorm:"primarykey"`
	Rev       string
	Since     *string
	Commit    *models.DbCID
	Prev      *models.DbCID
	NewHandle *string // NewHandle is only set if this is a handle change event

	Time   time.Time
	Blobs  []byte
	Repo   models.Uid
	Type   string
	Rebase bool

	// Active and Status are only set on RepoAccount events
	Active bool
	Status *string

	Ops []byte
}

func NewDbPersistence(db *gorm.DB, cs carstore.CarStore, options *Options) (*DbPersistence, error) {
	if err := db.AutoMigrate(&RepoEventRecord{}); err != nil {
		return nil, err
	}

	if options == nil {
		options = DefaultOptions()
	}

	uidCache, err := arc.NewARC[models.Uid, string](options.UIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create uid cache: %w", err)
	}

	didCache, err := arc.NewARC[string, models.Uid](options.DIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create did cache: %w", err)
	}

	p := DbPersistence{
		db:           db,
		cs:           cs,
		batchOptions: *options,
		batch:        []*PersistenceBatchItem{},
		uidCache:     uidCache,
		didCache:     didCache,
	}

	go p.batchFlusher()

	return &p, nil
}

func (p *DbPersistence) batchFlusher() {
	for {
		time.Sleep(p.batchOptions.CheckBatchInterval)

		p.lk.Lock()
		needsFlush := len(p.batch) > 0 &&
			(len(p.batch) >= p.batchOptions.MinBatchSize ||
				time.Since(p.lastFlush) >= p.batchOptions.MaxTimeBetweenFlush)
		p.lk.Unlock()

		if needsFlush {
			if err := p.Flush(context.Background()); err != nil {
				log.Error("failed to flush batch", "err", err)
			}
		}
	}
}

func (p *DbPersistence) SetEventBroadcaster(brc func(*events.XRPCStreamEvent)) {
	p.broadcast = brc
}

func (p *DbPersistence) Flush(ctx context.Context) error {
	p.lk.Lock()
	defer p.lk.Unlock()
	return p.flushBatchLocked(ctx)
}

func (p *DbPersistence) flushBatchLocked(ctx context.Context) error {
	// TODO: we technically don't need to hold the lock through the database
	// operation, all we need to do is swap the batch out, and ensure nobody
	// else tries to enter this function to flush another batch while we are
	// flushing. I'll leave that for a later optimization

	records := make([]*RepoEventRecord, len(p.batch))
	for i, item := range p.batch {
		records[i] = item.Record
	}

	if err := p.db.CreateInBatches(records, 50).Error; err != nil {
		return fmt.Errorf("failed to create records: %w", err)
	}

	for i, item := range records {
		e := p.batch[i].Event
		switch {
		case e.RepoCommit != nil:
			e.RepoCommit.Seq = int64(item.Seq)
		case e.RepoSync != nil:
			e.RepoSync.Seq = int64(item.Seq)
		case e.RepoIdentity != nil:
			e.RepoIdentity.Seq = int64(item.Seq)
		case e.RepoAccount != nil:
			e.RepoAccount.Seq = int64(item.Seq)
		default:
			return fmt.Errorf("unknown event type")
		}
		p.broadcast(e)
	}

	p.batch = []*PersistenceBatchItem{}
	p.lastFlush = time.Now()

	return nil
}

func (p *DbPersistence) AddItemToBatch(ctx context.Context, rec *RepoEventRecord, evt *events.XRPCStreamEvent) error {
	p.lk.Lock()
	defer p.lk.Unlock()
	p.batch = append(p.batch, &PersistenceBatchItem{
		Record: rec,
		Event:  evt,
	})

	if len(p.batch) >= p.batchOptions.MaxBatchSize {
		if err := p.flushBatchLocked(ctx); err != nil {
			return fmt.Errorf("failed to flush batch at max size: %w", err)
		}
	}

	return nil
}

func (p *DbPersistence) Persist(ctx context.Context, e *events.XRPCStreamEvent) error {
	var rer *RepoEventRecord
	var err error

	switch {
	case e.RepoCommit != nil:
		rer, err = p.RecordFromRepoCommit(ctx, e.RepoCommit)
		if err != nil {
			return err
		}
	case e.RepoSync != nil:
		rer, err = p.RecordFromRepoSync(ctx, e.RepoSync)
		if err != nil {
			return err
		}
	case e.RepoIdentity != nil:
		rer, err = p.RecordFromRepoIdentity(ctx, e.RepoIdentity)
		if err != nil {
			return err
		}
	case e.RepoAccount != nil:
		rer, err = p.RecordFromRepoAccount(ctx, e.RepoAccount)
		if err != nil {
			return err
		}
	default:
		return nil
	}

	if err := p.AddItemToBatch(ctx, rer, e); err != nil {
		return err
	}

	return nil
}

func (p *DbPersistence) RecordFromRepoIdentity(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity) (*RepoEventRecord, error) {
	t, err := time.Parse(util.ISO8601, evt.Time)
	if err != nil {
		return nil, err
	}

	uid, err := p.uidForDid(ctx, evt.Did)
	if err != nil {
		return nil, err
	}

	return &RepoEventRecord{
		Repo: uid,
		Type: "repo_identity",
		Time: t,
	}, nil
}

func (p *DbPersistence) RecordFromRepoAccount(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account) (*RepoEventRecord, error) {
	t, err := time.Parse(util.ISO8601, evt.Time)
	if err != nil {
		return nil, err
	}

	uid, err := p.uidForDid(ctx, evt.Did)
	if err != nil {
		return nil, err
	}

	return &RepoEventRecord{
		Repo:   uid,
		Type:   "repo_account",
		Time:   t,
		Active: evt.Active,
		Status: evt.Status,
	}, nil
}

func (p *DbPersistence) RecordFromRepoCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) (*RepoEventRecord, error) {
	// TODO: hack hack hack
	if len(evt.Ops) > 8192 {
		log.Error("(VERY BAD) truncating ops field in outgoing event", "len", len(evt.Ops))
		evt.Ops = evt.Ops[:8192]
	}

	uid, err := p.uidForDid(ctx, evt.Repo)
	if err != nil {
		return nil, err
	}

	var blobs []byte
	if len(evt.Blobs) > 0 {
		b, err := json.Marshal(evt.Blobs)
		if err != nil {
			return nil, err
		}
		blobs = b
	}

	t, err := time.Parse(util.ISO8601, evt.Time)
	if err != nil {
		return nil, err
	}

	rer := RepoEventRecord{
		Commit: &models.DbCID{CID: cid.Cid(evt.Commit)},
		//Prev
		Repo:   uid,
		Type:   "repo_append", // TODO: refactor to "#commit"? can "rebase" come through this path?
		Blobs:  blobs,
		Time:   t,
		Rebase: evt.Rebase,
		Rev:    evt.Rev,
		Since:  evt.Since,
	}

	opsb, err := json.Marshal(evt.Ops)
	if err != nil {
		return nil, err
	}
	rer.Ops = opsb

	return &rer, nil
}

func (p *DbPersistence) RecordFromRepoSync(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync) (*RepoEventRecord, error) {

	uid, err := p.uidForDid(ctx, evt.Did)
	if err != nil {
		return nil, err
	}

	t, err := time.Parse(util.ISO8601, evt.Time)
	if err != nil {
		return nil, err
	}

	rer := RepoEventRecord{
		Repo: uid,
		Type: "repo_sync",
		Time: t,
		Rev:  evt.Rev,
	}

	return &rer, nil
}

func (p *DbPersistence) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	pageSize := 1000

	for {
		rows, err := p.db.Model(&RepoEventRecord{}).Where("seq > ?", since).Order("seq asc").Limit(pageSize).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()

		hasRows := false

		batch := make([]*RepoEventRecord, 0, p.batchOptions.PlaybackBatchSize)
		for rows.Next() {
			hasRows = true

			var evt RepoEventRecord
			if err := p.db.ScanRows(rows, &evt); err != nil {
				return err
			}

			// Advance the since cursor
			since = int64(evt.Seq)

			batch = append(batch, &evt)

			if len(batch) >= p.batchOptions.PlaybackBatchSize {
				if err := p.hydrateBatch(ctx, batch, cb); err != nil {
					return err
				}

				batch = batch[:0]
			}
		}

		if len(batch) > 0 {
			if err := p.hydrateBatch(ctx, batch, cb); err != nil {
				return err
			}
		}

		if !hasRows {
			break
		}
	}

	return nil
}

func (p *DbPersistence) hydrateBatch(ctx context.Context, batch []*RepoEventRecord, cb func(*events.XRPCStreamEvent) error) error {
	evts := make([]*events.XRPCStreamEvent, len(batch))

	type Result struct {
		Event *events.XRPCStreamEvent
		Index int
		Err   error
	}

	resultChan := make(chan Result, len(batch))

	// Semaphore pattern for limiting concurrent goroutines
	sem := make(chan struct{}, p.batchOptions.HydrationConcurrency)
	var wg sync.WaitGroup

	for i, record := range batch {
		wg.Add(1)
		go func(i int, record *RepoEventRecord) {
			defer wg.Done()
			sem <- struct{}{}
			// release the semaphore at the end of the goroutine
			defer func() { <-sem }()

			var streamEvent *events.XRPCStreamEvent
			var err error

			switch {
			case record.Commit != nil:
				streamEvent, err = p.hydrateCommit(ctx, record)
			case record.Type == "repo_sync":
				streamEvent, err = p.hydrateSyncEvent(ctx, record)
			case record.Type == "repo_identity":
				streamEvent, err = p.hydrateIdentityEvent(ctx, record)
			case record.Type == "repo_account":
				streamEvent, err = p.hydrateAccountEvent(ctx, record)
			default:
				err = fmt.Errorf("unknown event type: %s", record.Type)
			}

			resultChan <- Result{Event: streamEvent, Index: i, Err: err}

		}(i, record)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	cur := 0
	for result := range resultChan {
		if result.Err != nil {
			return result.Err
		}

		evts[result.Index] = result.Event

		for ; cur < len(evts) && evts[cur] != nil; cur++ {
			if err := cb(evts[cur]); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *DbPersistence) uidForDid(ctx context.Context, did string) (models.Uid, error) {
	if uid, ok := p.didCache.Get(did); ok {
		return uid, nil
	}

	var u models.ActorInfo
	if err := p.db.First(&u, "did = ?", did).Error; err != nil {
		return 0, err
	}

	p.didCache.Add(did, u.Uid)

	return u.Uid, nil
}

func (p *DbPersistence) didForUid(ctx context.Context, uid models.Uid) (string, error) {
	if did, ok := p.uidCache.Get(uid); ok {
		return did, nil
	}

	var u models.ActorInfo
	if err := p.db.First(&u, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	p.uidCache.Add(uid, u.Did)

	return u.Did, nil
}

func (p *DbPersistence) hydrateIdentityEvent(ctx context.Context, rer *RepoEventRecord) (*events.XRPCStreamEvent, error) {
	did, err := p.didForUid(ctx, rer.Repo)
	if err != nil {
		return nil, err
	}

	return &events.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:  did,
			Time: rer.Time.Format(util.ISO8601),
		},
	}, nil
}

func (p *DbPersistence) hydrateAccountEvent(ctx context.Context, rer *RepoEventRecord) (*events.XRPCStreamEvent, error) {
	did, err := p.didForUid(ctx, rer.Repo)
	if err != nil {
		return nil, err
	}

	return &events.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Did:    did,
			Time:   rer.Time.Format(util.ISO8601),
			Active: rer.Active,
			Status: rer.Status,
		},
	}, nil
}

func (p *DbPersistence) hydrateCommit(ctx context.Context, rer *RepoEventRecord) (*events.XRPCStreamEvent, error) {
	if rer.Commit == nil {
		return nil, fmt.Errorf("commit is nil")
	}

	var blobs []string
	if len(rer.Blobs) > 0 {
		if err := json.Unmarshal(rer.Blobs, &blobs); err != nil {
			return nil, err
		}
	}
	var blobCIDs []lexutil.LexLink
	for _, b := range blobs {
		c, err := cid.Decode(b)
		if err != nil {
			return nil, err
		}
		blobCIDs = append(blobCIDs, lexutil.LexLink(c))
	}

	did, err := p.didForUid(ctx, rer.Repo)
	if err != nil {
		return nil, err
	}

	var ops []*comatproto.SyncSubscribeRepos_RepoOp
	if err := json.Unmarshal(rer.Ops, &ops); err != nil {
		return nil, err
	}

	out := &comatproto.SyncSubscribeRepos_Commit{
		Seq:    int64(rer.Seq),
		Repo:   did,
		Commit: lexutil.LexLink(rer.Commit.CID),
		Time:   rer.Time.Format(util.ISO8601),
		Blobs:  blobCIDs,
		Rebase: rer.Rebase,
		Ops:    ops,
		Rev:    rer.Rev,
		Since:  rer.Since,
	}

	cs, err := p.readCarSlice(ctx, rer)
	if err != nil {
		return nil, fmt.Errorf("read car slice (%s): %w", rer.Commit.CID, err)
	}

	if len(cs) > carstore.MaxSliceLength {
		out.TooBig = true
		out.Blocks = []byte{}
	} else {
		out.Blocks = cs
	}

	return &events.XRPCStreamEvent{RepoCommit: out}, nil
}

func (p *DbPersistence) hydrateSyncEvent(ctx context.Context, rer *RepoEventRecord) (*events.XRPCStreamEvent, error) {

	did, err := p.didForUid(ctx, rer.Repo)
	if err != nil {
		return nil, err
	}

	evt := &comatproto.SyncSubscribeRepos_Sync{
		Seq:  int64(rer.Seq),
		Did:  did,
		Time: rer.Time.Format(util.ISO8601),
		Rev:  rer.Rev,
	}

	cs, err := p.readCarSlice(ctx, rer)
	if err != nil {
		return nil, fmt.Errorf("read car slice: %w", err)
	}
	evt.Blocks = cs

	return &events.XRPCStreamEvent{RepoSync: evt}, nil
}

func (p *DbPersistence) readCarSlice(ctx context.Context, rer *RepoEventRecord) ([]byte, error) {

	buf := new(bytes.Buffer)
	if err := p.cs.ReadUserCar(ctx, rer.Repo, rer.Rev, true, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (p *DbPersistence) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	return p.deleteAllEventsForUser(ctx, usr)
}

func (p *DbPersistence) deleteAllEventsForUser(ctx context.Context, usr models.Uid) error {
	if err := p.db.Where("repo = ?", usr).Delete(&RepoEventRecord{}).Error; err != nil {
		return err
	}

	return nil
}

func (p *DbPersistence) Shutdown(context.Context) error {
	return nil
}
