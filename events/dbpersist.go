package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"
	cid "github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type DbPersistence struct {
	db *gorm.DB

	cs *carstore.CarStore
}

type RepoEventRecord struct {
	Seq    uint `gorm:"primarykey"`
	Commit util.DbCID
	Prev   *util.DbCID

	Time  time.Time
	Blobs []byte
	Repo  util.Uid
	Event string

	Ops []RepoOpRecord
}

type RepoOpRecord struct {
	ID                uint `gorm:"primarykey"`
	RepoEventRecordID uint
	Path              string
	Action            string
	Rec               *util.DbCID
}

func NewDbPersistence(db *gorm.DB, cs *carstore.CarStore) (*DbPersistence, error) {
	if err := db.AutoMigrate(&RepoEventRecord{}); err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&RepoOpRecord{}); err != nil {
		return nil, err
	}

	return &DbPersistence{
		db: db,
		cs: cs,
	}, nil
}

func (p *DbPersistence) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	if e.RepoAppend == nil {
		return nil
	}

	evt := e.RepoAppend

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
	if evt.Prev != nil && *evt.Prev != cid.Undef {
		prev = &util.DbCID{*evt.Prev}
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
		Commit: util.DbCID{evt.Commit},
		Prev:   prev,
		Repo:   uid,
		Event:  evt.Event,
		Blobs:  blobs,
		Time:   t,
	}

	for _, op := range evt.Ops {
		var rec *util.DbCID
		if op.Cid != nil && *op.Cid != cid.Undef {
			rec = &util.DbCID{*op.Cid}
		}
		rer.Ops = append(rer.Ops, RepoOpRecord{
			Path:   op.Path,
			Action: op.Action,
			Rec:    rec,
		})
	}
	if err := p.db.Create(&rer).Error; err != nil {
		return err
	}

	e.RepoAppend.Seq = int64(rer.Seq)

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

		var ops []RepoOpRecord
		if err := p.db.Find(&ops, "repo_event_record_id = ?", evt.Seq).Error; err != nil {
			return err
		}

		evt.Ops = ops

		ra, err := p.hydrateRepoEvent(ctx, &evt)
		if err != nil {
			return err
		}

		if err := cb(&XRPCStreamEvent{RepoAppend: ra}); err != nil {
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

func (p *DbPersistence) hydrateRepoEvent(ctx context.Context, rer *RepoEventRecord) (*RepoAppend, error) {
	var blobs []string
	if len(rer.Blobs) > 0 {
		if err := json.Unmarshal(rer.Blobs, &blobs); err != nil {
			return nil, err
		}
	}
	var blobCIDs []cid.Cid
	for _, b := range blobs {
		c, err := cid.Decode(b)
		if err != nil {
			return nil, err
		}
		blobCIDs = append(blobCIDs, c)
	}

	did, err := p.didForUid(ctx, rer.Repo)
	if err != nil {
		return nil, err
	}

	var prevCID *cid.Cid
	if rer != nil && rer.Prev != nil && rer.Prev.CID != cid.Undef {
		prevCID = &rer.Prev.CID
	}

	out := &RepoAppend{
		Seq:    int64(rer.Seq),
		Repo:   did,
		Commit: rer.Commit.CID,
		Prev:   prevCID,
		Time:   rer.Time.Format(util.ISO8601),
		Blobs:  blobCIDs,
		Event:  rer.Event,
	}

	for _, op := range rer.Ops {
		var recCID *cid.Cid
		if op.Rec != nil {
			recCID = &op.Rec.CID
		}

		out.Ops = append(out.Ops, &RepoOp{
			Path:   op.Path,
			Action: op.Action,
			Cid:    recCID,
		})
	}

	cs, err := p.readCarSlice(ctx, rer)
	if err != nil {
		return nil, fmt.Errorf("read car slice: %w", err)
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
	if rer.Prev != nil {
		early = rer.Prev.CID
	}

	buf := new(bytes.Buffer)
	if err := p.cs.ReadUserCar(ctx, rer.Repo, early, rer.Commit.CID, true, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
