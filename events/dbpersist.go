package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"

	cid "github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type PersistenceBatchItem struct {
	Record *RepoEventRecord
	Event  *XRPCStreamEvent
}

type BatchOptions struct {
	MaxBatchSize        int
	MinBatchSize        int
	MaxTimeBetweenFlush time.Duration
}

func DefaultBatchOptions() *BatchOptions {
	return &BatchOptions{
		MaxBatchSize:        200,
		MinBatchSize:        10,
		MaxTimeBetweenFlush: 500 * time.Millisecond,
	}
}

type DbPersistence struct {
	db *gorm.DB

	cs *carstore.CarStore

	lk sync.Mutex

	broadcast func(*XRPCStreamEvent)

	batch        []*PersistenceBatchItem
	batchOptions BatchOptions
	lastFlush    time.Time
}

type RepoEventRecord struct {
	Seq    uint `gorm:"primarykey"`
	Commit util.DbCID
	Prev   *util.DbCID

	Time   time.Time
	Blobs  []byte
	Repo   util.Uid
	Event  string
	Rebase bool

	Ops []byte
}

func NewDbPersistence(db *gorm.DB, cs *carstore.CarStore, batchOptions *BatchOptions) (*DbPersistence, error) {
	if err := db.AutoMigrate(&RepoEventRecord{}); err != nil {
		return nil, err
	}

	if batchOptions == nil {
		batchOptions = DefaultBatchOptions()
	}

	p := DbPersistence{
		db:           db,
		cs:           cs,
		batchOptions: *batchOptions,
		batch:        []*PersistenceBatchItem{},
	}

	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			p.lk.Lock()
			if len(p.batch) > 0 &&
				(len(p.batch) >= p.batchOptions.MinBatchSize ||
					time.Since(p.lastFlush) >= p.batchOptions.MaxTimeBetweenFlush) {
				p.lk.Unlock()
				if err := p.FlushBatch(context.Background()); err != nil {
					log.Errorf("failed to flush batch: %s", err)
				}
			} else {
				p.lk.Unlock()
			}
		}
	}()

	return &p, nil
}

func (p *DbPersistence) SetEventBroadcaster(brc func(*XRPCStreamEvent)) {
	p.broadcast = brc
}

func (p *DbPersistence) FlushBatch(ctx context.Context) error {
	p.lk.Lock()
	defer p.lk.Unlock()

	records := make([]*RepoEventRecord, len(p.batch))
	for i, item := range p.batch {
		records[i] = item.Record
	}

	if err := p.db.CreateInBatches(records, 50).Error; err != nil {
		return fmt.Errorf("failed to create records: %w", err)
	}

	for i, item := range records {
		e := p.batch[i].Event
		e.RepoCommit.Seq = int64(item.Seq)
		p.broadcast(e)
	}

	p.batch = []*PersistenceBatchItem{}
	p.lastFlush = time.Now()

	return nil
}

func (p *DbPersistence) AddItemToBatch(ctx context.Context, rec *RepoEventRecord, evt *XRPCStreamEvent) error {
	p.lk.Lock()
	if p.batch == nil {
		p.batch = []*PersistenceBatchItem{}
	}

	if len(p.batch) >= p.batchOptions.MaxBatchSize {
		p.lk.Unlock()
		if err := p.FlushBatch(ctx); err != nil {
			return fmt.Errorf("failed to flush batch at max size: %w", err)
		}
		p.lk.Lock()
	}

	p.batch = append(p.batch, &PersistenceBatchItem{
		Record: rec,
		Event:  evt,
	})

	p.lk.Unlock()

	return nil
}

func (p *DbPersistence) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	if e.RepoCommit == nil {
		return nil
	}

	evt := e.RepoCommit

	// TODO: hack hack hack
	if len(evt.Ops) > 8192 {
		log.Errorf("(VERY BAD) truncating ops field in outgoing event (len = %d)", len(evt.Ops))
		evt.Ops = evt.Ops[:8192]
	}

	uid, err := p.uidForDid(ctx, evt.Repo)
	if err != nil {
		return err
	}

	var prev *util.DbCID
	if evt.Prev != nil && evt.Prev.Defined() {
		prev = &util.DbCID{cid.Cid(*evt.Prev)}
	}

	var blobs []byte
	if len(evt.Blobs) > 0 {
		b, err := json.Marshal(evt.Blobs)
		if err != nil {
			return err
		}
		blobs = b
	}

	t, err := time.Parse(util.ISO8601, evt.Time)
	if err != nil {
		return err
	}

	rer := RepoEventRecord{
		Commit: util.DbCID{cid.Cid(evt.Commit)},
		Prev:   prev,
		Repo:   uid,
		Event:  "repo_append", // TODO: refactor to "#commit"? can "rebase" come through this path?
		Blobs:  blobs,
		Time:   t,
		Rebase: evt.Rebase,
	}

	opsb, err := json.Marshal(evt.Ops)
	if err != nil {
		return err
	}
	rer.Ops = opsb

	if err := p.AddItemToBatch(ctx, &rer, e); err != nil {
		return err
	}

	return nil
}

func (p *DbPersistence) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	rows, err := p.db.Model(RepoEventRecord{}).Where("seq > ?", since).Order("seq asc").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var evt RepoEventRecord
		if err := p.db.ScanRows(rows, &evt); err != nil {
			return err
		}

		ra, err := p.hydrateRepoEvent(ctx, &evt)
		if err != nil {
			return fmt.Errorf("hydrating event: %w", err)
		}

		if err := cb(&XRPCStreamEvent{RepoCommit: ra}); err != nil {
			return err
		}
	}

	return nil
}

func (p *DbPersistence) uidForDid(ctx context.Context, did string) (util.Uid, error) {
	var u models.ActorInfo
	if err := p.db.First(&u, "did = ?", did).Error; err != nil {
		return 0, err
	}

	return u.Uid, nil
}

func (p *DbPersistence) didForUid(ctx context.Context, uid util.Uid) (string, error) {
	var u models.ActorInfo
	if err := p.db.First(&u, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	return u.Did, nil
}

func (p *DbPersistence) hydrateRepoEvent(ctx context.Context, rer *RepoEventRecord) (*comatproto.SyncSubscribeRepos_Commit, error) {
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

	var prevCID *lexutil.LexLink
	if rer != nil && rer.Prev != nil && rer.Prev.CID.Defined() {
		tmp := lexutil.LexLink(rer.Prev.CID)
		prevCID = &tmp
	}

	var ops []*comatproto.SyncSubscribeRepos_RepoOp
	if err := json.Unmarshal(rer.Ops, &ops); err != nil {
		return nil, err
	}

	out := &comatproto.SyncSubscribeRepos_Commit{
		Seq:    int64(rer.Seq),
		Repo:   did,
		Commit: lexutil.LexLink(rer.Commit.CID),
		Prev:   prevCID,
		Time:   rer.Time.Format(util.ISO8601),
		Blobs:  blobCIDs,
		Rebase: rer.Rebase,
		Ops:    ops,
		// TODO: there was previously an Event field here. are these all Commit, or are some other events?
	}

	cs, err := p.readCarSlice(ctx, rer)
	if err != nil {
		return nil, fmt.Errorf("read car slice (%s): %w", rer.Commit.CID, err)
	}

	if len(cs) > carstore.MaxSliceLength {
		out.TooBig = true
	} else {
		out.Blocks = cs
	}

	return out, nil
}

func (p *DbPersistence) readCarSlice(ctx context.Context, rer *RepoEventRecord) ([]byte, error) {

	var early cid.Cid
	if rer.Prev != nil && !rer.Rebase {
		early = rer.Prev.CID
	}

	buf := new(bytes.Buffer)
	if err := p.cs.ReadUserCar(ctx, rer.Repo, early, rer.Commit.CID, true, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (p *DbPersistence) TakeDownRepo(ctx context.Context, usr util.Uid) error {
	return p.deleteAllEventsForUser(ctx, usr)
}

func (p *DbPersistence) deleteAllEventsForUser(ctx context.Context, usr util.Uid) error {
	if err := p.db.Where("repo = ?", usr).Delete(&RepoEventRecord{}).Error; err != nil {
		return err
	}

	return nil
}

func (p *DbPersistence) RebaseRepoEvents(ctx context.Context, usr util.Uid) error {
	// a little weird that this is the same action as a takedown
	return p.deleteAllEventsForUser(ctx, usr)
}
