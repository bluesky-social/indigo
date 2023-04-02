package repomgr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-car"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var log = logging.Logger("repomgr")

func NewRepoManager(db *gorm.DB, cs *carstore.CarStore, kmgr KeyManager) *RepoManager {
	db.AutoMigrate(RepoHead{})

	return &RepoManager{
		db:        db,
		cs:        cs,
		userLocks: make(map[util.Uid]*userLock),
		kmgr:      kmgr,
	}
}

type KeyManager interface {
	VerifyUserSignature(context.Context, string, []byte, []byte) error
	SignForUser(context.Context, string, []byte) ([]byte, error)
}

func (rm *RepoManager) SetEventHandler(cb func(context.Context, *RepoEvent)) {
	rm.events = cb
}

type RepoManager struct {
	cs   *carstore.CarStore
	db   *gorm.DB
	kmgr KeyManager

	lklk      sync.Mutex
	userLocks map[util.Uid]*userLock

	events func(context.Context, *RepoEvent)
}

type ActorInfo struct {
	Did         string
	Handle      string
	DisplayName string
	Type        string
}

type RepoEvent struct {
	User      util.Uid
	OldRoot   *cid.Cid
	NewRoot   cid.Cid
	RepoSlice []byte
	PDS       uint
	Ops       []RepoOp
}

type RepoOp struct {
	Kind       EventKind
	Collection string
	Rkey       string
	RecCid     *cid.Cid
	Record     any
	ActorInfo  *ActorInfo
}

type EventKind string

const (
	EvtKindCreateRecord = EventKind("create")
	EvtKindUpdateRecord = EventKind("update")
	EvtKindDeleteRecord = EventKind("delete")
	EvtKindInitActor    = EventKind("init")
)

type RepoHead struct {
	gorm.Model
	Usr  util.Uid `gorm:"uniqueIndex"`
	Root string
}

type userLock struct {
	lk    sync.Mutex
	count int
}

func (rm *RepoManager) lockUser(ctx context.Context, user util.Uid) func() {
	ctx, span := otel.Tracer("repoman").Start(ctx, "userLock")
	defer span.End()

	rm.lklk.Lock()

	ulk, ok := rm.userLocks[user]
	if !ok {
		ulk = &userLock{}
		rm.userLocks[user] = ulk
	}

	ulk.count++

	rm.lklk.Unlock()

	ulk.lk.Lock()

	return func() {
		rm.lklk.Lock()

		ulk.lk.Unlock()
		ulk.count--

		if ulk.count == 0 {
			delete(rm.userLocks, user)
		}
		rm.lklk.Unlock()
	}
}

func (rm *RepoManager) getUserRepoHead(ctx context.Context, user util.Uid) (cid.Cid, error) {
	ctx, span := otel.Tracer("repoman").Start(ctx, "getUserRepoHead")
	defer span.End()

	var headrec RepoHead
	if err := rm.db.Find(&headrec, "usr = ?", user).Error; err != nil {
		return cid.Undef, err
	}

	if headrec.ID == 0 {
		return cid.Undef, gorm.ErrRecordNotFound
	}

	cc, err := cid.Decode(headrec.Root)
	if err != nil {
		return cid.Undef, err
	}

	return cc, nil
}

func (rm *RepoManager) updateUserRepoHead(ctx context.Context, user util.Uid, root cid.Cid) error {
	if err := rm.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "usr"}},
		DoUpdates: clause.AssignmentColumns([]string{"root"}),
	}).Create(&RepoHead{
		Usr:  user,
		Root: root.String(),
	}).Error; err != nil {
		return err
	}

	return nil
}

func (rm *RepoManager) CarStore() *carstore.CarStore {
	return rm.cs
}

func (rm *RepoManager) CreateRecord(ctx context.Context, user util.Uid, collection string, rec cbg.CBORMarshaler) (string, cid.Cid, error) {
	ctx, span := otel.Tracer("repoman").Start(ctx, "CreateRecord")
	defer span.End()

	unlock := rm.lockUser(ctx, user)
	defer unlock()

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return "", cid.Undef, err
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, &head)
	if err != nil {
		return "", cid.Undef, err
	}

	r, err := repo.OpenRepo(ctx, ds, head, true)
	if err != nil {
		return "", cid.Undef, err
	}

	cc, tid, err := r.CreateRecord(ctx, collection, rec)
	if err != nil {
		return "", cid.Undef, err
	}

	nroot, err := r.Commit(ctx, rm.kmgr.SignForUser)
	if err != nil {
		return "", cid.Undef, err
	}

	rslice, err := ds.CloseWithRoot(ctx, nroot)
	if err != nil {
		return "", cid.Undef, fmt.Errorf("close with root: %w", err)
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, user, nroot); err != nil {
		return "", cid.Undef, fmt.Errorf("updating user head: %w", err)
	}

	var oldroot *cid.Cid
	if head.Defined() {
		oldroot = &head
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: oldroot,
			NewRoot: nroot,
			Ops: []RepoOp{{
				Kind:       EvtKindCreateRecord,
				Collection: collection,
				Rkey:       tid,
				Record:     rec,
				RecCid:     &cc,
			}},
			RepoSlice: rslice,
		})
	}

	return collection + "/" + tid, cc, nil
}

func (rm *RepoManager) UpdateRecord(ctx context.Context, user util.Uid, collection, rkey string, rec cbg.CBORMarshaler) (cid.Cid, error) {
	ctx, span := otel.Tracer("repoman").Start(ctx, "UpdateRecord")
	defer span.End()

	unlock := rm.lockUser(ctx, user)
	defer unlock()

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return cid.Undef, err
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, &head)
	if err != nil {
		return cid.Undef, err
	}

	r, err := repo.OpenRepo(ctx, ds, head, true)
	if err != nil {
		return cid.Undef, err
	}

	rpath := collection + "/" + rkey
	cc, err := r.PutRecord(ctx, rpath, rec)
	if err != nil {
		return cid.Undef, err
	}

	nroot, err := r.Commit(ctx, rm.kmgr.SignForUser)
	if err != nil {
		return cid.Undef, err
	}

	rslice, err := ds.CloseWithRoot(ctx, nroot)
	if err != nil {
		return cid.Undef, fmt.Errorf("close with root: %w", err)
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, user, nroot); err != nil {
		return cid.Undef, fmt.Errorf("updating user head: %w", err)
	}

	var oldroot *cid.Cid
	if head.Defined() {
		oldroot = &head
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: oldroot,
			NewRoot: nroot,
			Ops: []RepoOp{{
				Kind:       EvtKindUpdateRecord,
				Collection: collection,
				Rkey:       rkey,
				Record:     rec,
				RecCid:     &cc,
			}},
			RepoSlice: rslice,
		})
	}

	return cc, nil
}

func (rm *RepoManager) DeleteRecord(ctx context.Context, user util.Uid, collection, rkey string) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "DeleteRecord")
	defer span.End()

	unlock := rm.lockUser(ctx, user)
	defer unlock()

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return err
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, &head)
	if err != nil {
		return err
	}

	r, err := repo.OpenRepo(ctx, ds, head, true)
	if err != nil {
		return err
	}

	rpath := collection + "/" + rkey
	if err := r.DeleteRecord(ctx, rpath); err != nil {
		return err
	}

	nroot, err := r.Commit(ctx, rm.kmgr.SignForUser)
	if err != nil {
		return err
	}

	rslice, err := ds.CloseWithRoot(ctx, nroot)
	if err != nil {
		return fmt.Errorf("close with root: %w", err)
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, user, nroot); err != nil {
		return fmt.Errorf("updating user head: %w", err)
	}
	var oldroot *cid.Cid
	if head.Defined() {
		oldroot = &head
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: oldroot,
			NewRoot: nroot,
			Ops: []RepoOp{{
				Kind:       EvtKindDeleteRecord,
				Collection: collection,
				Rkey:       rkey,
			}},
			RepoSlice: rslice,
		})
	}

	return nil

}

func (rm *RepoManager) InitNewActor(ctx context.Context, user util.Uid, handle, did, displayname string, declcid, actortype string) error {
	unlock := rm.lockUser(ctx, user)
	defer unlock()

	if did == "" {
		return fmt.Errorf("must specify did for new actor")
	}

	if user == 0 {
		return fmt.Errorf("must specify unique non-zero id for new actor")
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, nil)
	if err != nil {
		return err
	}

	r := repo.NewRepo(ctx, did, ds)

	profile := &bsky.ActorProfile{
		DisplayName: &displayname,
	}

	_, err = r.PutRecord(ctx, "app.bsky.actor.profile/self", profile)
	if err != nil {
		return fmt.Errorf("setting initial actor profile: %w", err)
	}

	root, err := r.Commit(ctx, rm.kmgr.SignForUser)
	if err != nil {
		return fmt.Errorf("committing repo for actor init: %w", err)
	}

	rslice, err := ds.CloseWithRoot(ctx, root)
	if err != nil {
		return err
	}

	if err := rm.db.Create(&RepoHead{
		Usr:  user,
		Root: root.String(),
	}).Error; err != nil {
		return err
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			NewRoot: root,
			Ops: []RepoOp{{
				Kind: EvtKindInitActor,
				ActorInfo: &ActorInfo{
					Did:         did,
					Handle:      handle,
					DisplayName: displayname,
					Type:        actortype,
				},
			}},
			RepoSlice: rslice,
		})
	}

	return nil
}

func (rm *RepoManager) GetRepoRoot(ctx context.Context, user util.Uid) (cid.Cid, error) {
	unlock := rm.lockUser(ctx, user)
	defer unlock()

	return rm.getUserRepoHead(ctx, user)
}

func (rm *RepoManager) ReadRepo(ctx context.Context, user util.Uid, earlyCid, lateCid cid.Cid, w io.Writer) error {
	return rm.cs.ReadUserCar(ctx, user, earlyCid, lateCid, true, w)
}

func (rm *RepoManager) GetRecord(ctx context.Context, user util.Uid, collection string, rkey string, maybeCid cid.Cid) (cid.Cid, cbg.CBORMarshaler, error) {
	bs, err := rm.cs.ReadOnlySession(user)
	if err != nil {
		return cid.Undef, nil, err
	}

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return cid.Undef, nil, err
	}

	r, err := repo.OpenRepo(ctx, bs, head, true)
	if err != nil {
		return cid.Undef, nil, err
	}

	ocid, val, err := r.GetRecord(ctx, collection+"/"+rkey)
	if err != nil {
		return cid.Undef, nil, err
	}

	if maybeCid.Defined() && ocid != maybeCid {
		return cid.Undef, nil, fmt.Errorf("record at specified key had different CID than expected")
	}

	return ocid, val, nil
}

func (rm *RepoManager) GetProfile(ctx context.Context, uid util.Uid) (*bsky.ActorProfile, error) {
	bs, err := rm.cs.ReadOnlySession(uid)
	if err != nil {
		return nil, err
	}

	head, err := rm.getUserRepoHead(ctx, uid)
	if err != nil {
		return nil, err
	}

	r, err := repo.OpenRepo(ctx, bs, head, true)
	if err != nil {
		return nil, err
	}

	_, val, err := r.GetRecord(ctx, "app.bsky.actor.profile/self")
	if err != nil {
		return nil, err
	}

	ap, ok := val.(*bsky.ActorProfile)
	if !ok {
		return nil, fmt.Errorf("found wrong type in actor profile location in tree")
	}

	return ap, nil
}

func (rm *RepoManager) HandleExternalUserEvent(ctx context.Context, pdsid uint, uid util.Uid, did string, prev *cid.Cid, carslice []byte) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "HandleExternalUserEvent")
	defer span.End()

	log.Infof("HandleExternalUserEvent: %d %d %s", pdsid, uid, prev)

	unlock := rm.lockUser(ctx, uid)
	defer unlock()

	root, ds, err := rm.cs.ImportSlice(ctx, uid, prev, carslice)
	if err != nil {
		return fmt.Errorf("importing external carslice: %w", err)
	}

	r, err := repo.OpenRepo(ctx, ds, root, true)
	if err != nil {
		return fmt.Errorf("opening external user repo (%d, root=%s): %w", uid, root, err)
	}

	repoDid := r.RepoDid()

	if did != repoDid {
		return fmt.Errorf("DID in repo did not match (%q != %q)", did, repoDid)
	}

	scom := r.SignedCommit()

	usc := scom.Unsigned()
	sb, err := usc.BytesForSigning()
	if err != nil {
		return fmt.Errorf("commit serialization failed: %w", err)
	}
	if err := rm.kmgr.VerifyUserSignature(ctx, repoDid, scom.Sig, sb); err != nil {
		return fmt.Errorf("signature check failed: %w", err)
	}

	log.Infow("external event", "uid", uid)

	var pcid cid.Cid
	if prev != nil {
		pcid = *prev
	}

	ops, err := r.DiffSince(ctx, pcid)
	if err != nil {
		return fmt.Errorf("calculating operations in event: %w", err)
	}
	var evtops []RepoOp

	if prev == nil {
		// send an implicit init actor event
		var ai models.ActorInfo
		if err := rm.db.First(&ai, "uid = ?", uid).Error; err != nil {
			return fmt.Errorf("expected initialized user: %w", err)
		}

		evtops = append(evtops, RepoOp{
			Kind: EvtKindInitActor,
			ActorInfo: &ActorInfo{
				Did:         ai.Did,
				Handle:      ai.Handle,
				DisplayName: ai.DisplayName,
				Type:        ai.Type,
			},
		})
	}

	for _, op := range ops {
		parts := strings.SplitN(op.Rpath, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid rpath in mst diff, must have collection and rkey")
		}

		switch op.Op {
		case "add":
			recid, rec, err := r.GetRecord(ctx, op.Rpath)
			if err != nil {
				return fmt.Errorf("reading changed record from car slice: %w", err)
			}

			evtops = append(evtops, RepoOp{
				Kind:       EvtKindCreateRecord,
				Collection: parts[0],
				Rkey:       parts[1],
				Record:     rec,
				RecCid:     &recid,
			})
			/*
				case EvtKindInitActor:
					var ai models.ActorInfo
					if err := rm.db.First(&ai, "id = ?", uid).Error; err != nil {
						return fmt.Errorf("expected initialized user: %w", err)
					}

					evtops = append(evtops, RepoOp{
						Kind: EvtKindInitActor,
						ActorInfo: &ActorInfo{
							Did:         ai.Did,
							Handle:      ai.Handle,
							DisplayName: ai.DisplayName,
							Type:        ai.Type,
						},
					})
			*/
		case "mut":
			recid, rec, err := r.GetRecord(ctx, op.Rpath)
			if err != nil {
				return fmt.Errorf("reading changed record from car slice: %w", err)
			}

			evtops = append(evtops, RepoOp{
				Kind:       EvtKindUpdateRecord,
				Collection: parts[0],
				Rkey:       parts[1],
				Record:     rec,
				RecCid:     &recid,
			})
		case "del":
			evtops = append(evtops, RepoOp{
				Kind:       EvtKindDeleteRecord,
				Collection: parts[0],
				Rkey:       parts[1],
			})
		default:
			return fmt.Errorf("unrecognized external user event kind: %q", op.Op)
		}
	}

	rslice, err := ds.CloseWithRoot(ctx, root)
	if err != nil {
		return fmt.Errorf("close with root: %w", err)
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, uid, root); err != nil {
		return fmt.Errorf("updating user head: %w", err)
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:      uid,
			OldRoot:   prev,
			NewRoot:   root,
			Ops:       evtops,
			RepoSlice: rslice,
			PDS:       pdsid,
		})
	}

	return nil
}

func rkeyForCollection(collection string) string {
	return repo.NextTID()
}

func (rm *RepoManager) BatchWrite(ctx context.Context, user util.Uid, writes []*atproto.RepoApplyWrites_Input_Writes_Elem) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "BatchWrite")
	defer span.End()

	unlock := rm.lockUser(ctx, user)
	defer unlock()

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return err
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, &head)
	if err != nil {
		return err
	}

	r, err := repo.OpenRepo(ctx, ds, head, true)
	if err != nil {
		return err
	}

	var ops []RepoOp
	for _, w := range writes {
		switch {
		case w.RepoApplyWrites_Create != nil:
			c := w.RepoApplyWrites_Create
			var rkey string
			if c.Rkey != nil {
				rkey = *c.Rkey
			} else {
				rkey = rkeyForCollection(c.Collection)
			}

			nsid := c.Collection + "/" + rkey
			cc, err := r.PutRecord(ctx, nsid, c.Value.Val)
			if err != nil {
				return err
			}

			ops = append(ops, RepoOp{
				Kind:       EvtKindCreateRecord,
				Collection: c.Collection,
				Rkey:       rkey,
				RecCid:     &cc,
				Record:     c.Value.Val,
			})
		case w.RepoApplyWrites_Update != nil:
			u := w.RepoApplyWrites_Update

			cc, err := r.PutRecord(ctx, u.Collection+"/"+u.Rkey, u.Value.Val)
			if err != nil {
				return err
			}

			ops = append(ops, RepoOp{
				Kind:       EvtKindUpdateRecord,
				Collection: u.Collection,
				Rkey:       u.Rkey,
				RecCid:     &cc,
				Record:     u.Value.Val,
			})
		case w.RepoApplyWrites_Delete != nil:
			d := w.RepoApplyWrites_Delete

			if err := r.DeleteRecord(ctx, d.Collection+"/"+d.Rkey); err != nil {
				return err
			}

			ops = append(ops, RepoOp{
				Kind:       EvtKindDeleteRecord,
				Collection: d.Collection,
				Rkey:       d.Rkey,
			})
		default:
			return fmt.Errorf("no operation set in write enum")
		}
	}

	nroot, err := r.Commit(ctx, rm.kmgr.SignForUser)
	if err != nil {
		return err
	}

	rslice, err := ds.CloseWithRoot(ctx, nroot)
	if err != nil {
		return fmt.Errorf("close with root: %w", err)
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, user, nroot); err != nil {
		return fmt.Errorf("updating user head: %w", err)
	}

	var oldroot *cid.Cid
	if head.Defined() {
		oldroot = &head
	}

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:      user,
			OldRoot:   oldroot,
			NewRoot:   nroot,
			RepoSlice: rslice,
			Ops:       ops,
		})
	}

	return nil
}

func (rm *RepoManager) ImportNewRepo(ctx context.Context, user util.Uid, repoDid string, r io.Reader, oldest cid.Cid) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "ImportNewRepo")
	defer span.End()

	unlock := rm.lockUser(ctx, user)
	defer unlock()

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	cshead, err := rm.cs.GetUserRepoHead(ctx, user)
	if err != nil {
		return err
	}

	if head != cshead {
		return fmt.Errorf("mismatch between carstore head tracking and repomgr: %s != %s", head, cshead)
	}

	if head != oldest {
		// TODO: we could probably just deal with this
		return fmt.Errorf("ImportNewRepo called with incorrect base")
	}

	err = rm.processNewRepo(ctx, user, r, head, func(ctx context.Context, old, nu cid.Cid, finish func(context.Context) ([]byte, error), bs blockstore.Blockstore) error {
		r, err := repo.OpenRepo(ctx, bs, nu, true)
		if err != nil {
			return fmt.Errorf("opening new repo: %w", err)
		}

		scom := r.SignedCommit()

		usc := scom.Unsigned()
		sb, err := usc.BytesForSigning()
		if err != nil {
			return fmt.Errorf("commit serialization failed: %w", err)
		}
		if err := rm.kmgr.VerifyUserSignature(ctx, repoDid, scom.Sig, sb); err != nil {
			return fmt.Errorf("new user signature check failed: %w", err)
		}

		diffops, err := r.DiffSince(ctx, old)
		if err != nil {
			return fmt.Errorf("diff trees: %w", err)
		}

		var ops []RepoOp
		for _, op := range diffops {
			out, err := processOp(ctx, bs, op)
			if err != nil {
				log.Errorw("failed to process repo op", "err", err, "path", op.Rpath)
			}

			if out != nil {
				ops = append(ops, *out)
			}
		}

		slice, err := finish(ctx)
		if err != nil {
			return err
		}

		if len(slice) > carstore.MaxSliceLength {
			log.Warnw("output slice too large", "len", len(slice), "user", user, "old", old, "new", nu)
		}

		if err := rm.updateUserRepoHead(ctx, user, nu); err != nil {
			// TODO: this will lead to things being in an inconsistent state
			return fmt.Errorf("failed to update repo head: %w", err)
		}
		var oldroot *cid.Cid
		if old.Defined() {
			oldroot = &old
		}

		if rm.events != nil {
			rm.events(ctx, &RepoEvent{
				User:      user,
				OldRoot:   oldroot,
				NewRoot:   nu,
				RepoSlice: slice,
				Ops:       ops,
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("process new repo (current head: %s): %w:", head, err)
	}

	return nil
}

func processOp(ctx context.Context, bs blockstore.Blockstore, op *mst.DiffOp) (*RepoOp, error) {
	parts := strings.SplitN(op.Rpath, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("repo mst had invalid rpath: %q", op.Rpath)
	}

	switch op.Op {
	case "add", "mut":
		blk, err := bs.Get(ctx, op.NewCid)
		if err != nil {
			return nil, err
		}

		rec, err := lexutil.CborDecodeValue(blk.RawData())
		if err != nil {
			if errors.Is(err, lexutil.ErrUnrecognizedType) {
				log.Warnf("failed processing repo diff: %s", err)
				return nil, nil
			}

			return nil, err
		}

		kind := EvtKindCreateRecord
		if op.Op == "mut" {
			kind = EvtKindUpdateRecord
		}

		return &RepoOp{
			Kind:       kind,
			Collection: parts[0],
			Rkey:       parts[1],
			RecCid:     &op.NewCid,
			Record:     rec,
		}, nil
	case "del":
		return &RepoOp{
			Kind:       EvtKindDeleteRecord,
			Collection: parts[0],
			Rkey:       parts[1],
			RecCid:     nil,
		}, nil

	default:
		return nil, fmt.Errorf("diff returned invalid op type: %q", op.Op)
	}
}

func (rm *RepoManager) processNewRepo(ctx context.Context, user util.Uid, r io.Reader, until cid.Cid, cb func(ctx context.Context, old, nu cid.Cid, finish func(context.Context) ([]byte, error), bs blockstore.Blockstore) error) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "ImportNewRepo")
	defer span.End()

	carr, err := car.NewCarReader(r)
	if err != nil {
		return err
	}

	if len(carr.Header.Roots) != 1 {
		return fmt.Errorf("invalid car file, header must have a single root (has %d)", len(carr.Header.Roots))
	}

	membs := blockstore.NewBlockstore(datastore.NewMapDatastore())

	for {
		blk, err := carr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if err := membs.Put(ctx, blk); err != nil {
			return err
		}
	}

	head := &carr.Header.Roots[0]

	var commits []cid.Cid
	for head != nil && *head != until {
		commits = append(commits, *head)
		rep, err := repo.OpenRepo(ctx, membs, *head, true)
		if err != nil {
			return fmt.Errorf("opening repo for backwalk (%d commits, until: %s, head: %s, carRoot: %s): %w", len(commits), until, *head, carr.Header.Roots[0], err)
		}

		prev, err := rep.PrevCommit(ctx)
		if err != nil {
			return fmt.Errorf("prevCommit: %w", err)
		}

		head = prev
	}

	if until.Defined() && (head == nil || *head != until) {
		// TODO: this shouldnt be happening, but i've seen some log messages
		// suggest that it might. Leaving this here to discover any cases where
		// it does.
		log.Errorw("reached end of walkback without finding our 'until' commit",
			"until", until,
			"root", carr.Header.Roots[0],
			"commits", len(commits),
			"head", head,
		)
	}

	// now we need to generate repo slices for each commit

	seen := make(map[cid.Cid]bool)

	if until.Defined() {
		seen[until] = true
	}

	cbs := membs
	if until.Defined() {
		bs, err := rm.cs.ReadOnlySession(user)
		if err != nil {
			return err
		}

		// TODO: we technically only need this for the 'next' commit to diff against our current head.
		cbs = util.NewReadThroughBstore(bs, membs)
	}

	prev := until
	for i := len(commits) - 1; i >= 0; i-- {
		root := commits[i]
		// TODO: if there are blocks that get convergently recreated throughout
		// the repos lifecycle, this will end up erroneously not including
		// them. We should compute the set of blocks needed to read any repo
		// ops that happened in the commit and use that for our 'output' blocks
		cids, err := walkTree(ctx, seen, root, membs, true)
		if err != nil {
			return fmt.Errorf("walkTree: %w", err)
		}

		var prevptr *cid.Cid
		if prev.Defined() {
			prevptr = &prev
		}
		ds, err := rm.cs.NewDeltaSession(ctx, user, prevptr)
		if err != nil {
			return fmt.Errorf("opening delta session (%d / %d): %w", i, len(commits)-1, err)
		}

		for _, c := range cids {
			blk, err := membs.Get(ctx, c)
			if err != nil {
				return fmt.Errorf("copying walked cids to carstore: %w", err)
			}

			if err := ds.Put(ctx, blk); err != nil {
				return err
			}
		}

		finish := func(ctx context.Context) ([]byte, error) {
			return ds.CloseWithRoot(ctx, root)
		}

		if err := cb(ctx, prev, root, finish, cbs); err != nil {
			return fmt.Errorf("cb errored (%d/%d) root: %s, prev: %s: %w", i, len(commits)-1, root, prev, err)
		}

		prev = root
	}

	return nil
}

// walkTree returns all cids linked recursively by the root, skipping any cids
// in the 'skip' map, and not erroring on 'not found' if prevMissing is set
func walkTree(ctx context.Context, skip map[cid.Cid]bool, root cid.Cid, bs blockstore.Blockstore, prevMissing bool) ([]cid.Cid, error) {
	// TODO: what if someone puts non-cbor links in their repo?
	if root.Prefix().Codec != cid.DagCBOR {
		return nil, fmt.Errorf("can only handle dag-cbor objects in repos (%s is %d)", root, root.Prefix().Codec)
	}

	blk, err := bs.Get(ctx, root)
	if err != nil {
		return nil, err
	}

	var links []cid.Cid
	if err := cbg.ScanForLinks(bytes.NewReader(blk.RawData()), func(c cid.Cid) {
		if c.Prefix().Codec == cid.Raw {
			log.Debugw("skipping 'raw' CID in record", "recordCid", root, "rawCid", c)
			return
		}
		if skip[c] {
			return
		}

		links = append(links, c)
		skip[c] = true

		return
	}); err != nil {
		return nil, err
	}

	out := []cid.Cid{root}
	skip[root] = true

	// TODO: should do this non-recursive since i expect these may get deep
	for _, c := range links {
		sub, err := walkTree(ctx, skip, c, bs, prevMissing)
		if err != nil {
			if prevMissing && !ipld.IsNotFound(err) {
				return nil, err
			}
		}

		out = append(out, sub...)
	}

	return out, nil
}
