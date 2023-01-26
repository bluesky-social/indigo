package repomgr

import (
	"context"
	"fmt"
	"io"
	"sync"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	apibsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/types"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var log = logging.Logger("repomgr")

func NewRepoManager(db *gorm.DB, cs *carstore.CarStore) *RepoManager {
	db.AutoMigrate(RepoHead{})

	return &RepoManager{
		db:        db,
		cs:        cs,
		userLocks: make(map[uint]*userLock),
	}
}

func (rm *RepoManager) SetEventHandler(cb func(context.Context, *RepoEvent)) {
	rm.events = cb
}

type RepoManager struct {
	cs *carstore.CarStore
	db *gorm.DB

	lklk      sync.Mutex
	userLocks map[uint]*userLock

	events func(context.Context, *RepoEvent)
}

type ActorInfo struct {
	Did         string
	Handle      string
	DisplayName string
	DeclRefCid  string
	Type        string
}

type RepoEvent struct {
	User      uint
	OldRoot   cid.Cid
	NewRoot   cid.Cid
	RepoSlice []byte
	PDS       uint
	Ops       []RepoOp
}

type RepoOp struct {
	Kind       EventKind
	Collection string
	Rkey       string
	RecCid     cid.Cid
	Record     any
	ActorInfo  *ActorInfo
}

type EventKind string

const (
	EvtKindCreateRecord = EventKind("createRecord")
	EvtKindUpdateRecord = EventKind("updateRecord")
	EvtKindDeleteRecord = EventKind("deleteRecord")
	EvtKindInitActor    = EventKind("initActor")
)

type RepoHead struct {
	gorm.Model
	Usr  uint `gorm:"uniqueIndex"`
	Root string
}

type userLock struct {
	lk    sync.Mutex
	count int
}

func (rm *RepoManager) lockUser(ctx context.Context, user uint) func() {
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

func (rm *RepoManager) getUserRepoHead(ctx context.Context, user uint) (cid.Cid, error) {
	ctx, span := otel.Tracer("repoman").Start(ctx, "getUserRepoHead")
	defer span.End()

	var headrec RepoHead
	if err := rm.db.First(&headrec, "usr = ?", user).Error; err != nil {
		return cid.Undef, err
	}

	cc, err := cid.Decode(headrec.Root)
	if err != nil {
		return cid.Undef, err
	}

	return cc, nil
}

func (rm *RepoManager) updateUserRepoHead(ctx context.Context, user uint, root cid.Cid) error {
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

func (rm *RepoManager) CreateRecord(ctx context.Context, user uint, collection string, rec cbg.CBORMarshaler) (string, cid.Cid, error) {
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

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return "", cid.Undef, err
	}

	cc, tid, err := r.CreateRecord(ctx, collection, rec)
	if err != nil {
		return "", cid.Undef, err
	}

	nroot, err := r.Commit(ctx)
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

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: head,
			NewRoot: nroot,
			Ops: []RepoOp{{
				Kind:       EvtKindCreateRecord,
				Collection: collection,
				Rkey:       tid,
				Record:     rec,
				RecCid:     cc,
			}},
			RepoSlice: rslice,
		})
	}

	return collection + "/" + tid, cc, nil
}

func (rm *RepoManager) UpdateRecord(ctx context.Context, user uint, collection, rkey string, rec cbg.CBORMarshaler) (cid.Cid, error) {
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

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return cid.Undef, err
	}

	rpath := collection + "/" + rkey
	cc, err := r.PutRecord(ctx, rpath, rec)
	if err != nil {
		return cid.Undef, err
	}

	nroot, err := r.Commit(ctx)
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

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: head,
			NewRoot: nroot,
			Ops: []RepoOp{{
				Kind:       EvtKindUpdateRecord,
				Collection: collection,
				Rkey:       rkey,
				Record:     rec,
				RecCid:     cc,
			}},
			RepoSlice: rslice,
		})
	}

	return cc, nil
}

func (rm *RepoManager) DeleteRecord(ctx context.Context, user uint, collection, rkey string) error {
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

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return err
	}

	rpath := collection + "/" + rkey
	if err := r.DeleteRecord(ctx, rpath); err != nil {
		return err
	}

	nroot, err := r.Commit(ctx)
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

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:    user,
			OldRoot: head,
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

func (rm *RepoManager) InitNewActor(ctx context.Context, user uint, handle, did, displayname string, declcid, actortype string) error {
	unlock := rm.lockUser(ctx, user)
	defer unlock()

	if did == "" {
		return fmt.Errorf("must specify did for new actor")
	}

	if user == 0 {
		return fmt.Errorf("must specify unique non-zero id for new actor")
	}

	ds, err := rm.cs.NewDeltaSession(ctx, user, &cid.Undef)
	if err != nil {
		return err
	}

	r := repo.NewRepo(ctx, ds)

	profile := &apibsky.ActorProfile{
		DisplayName: displayname,
	}

	_, err = r.PutRecord(ctx, "app.bsky.actor.profile/self", profile)
	if err != nil {
		return fmt.Errorf("setting initial actor profile: %w", err)
	}

	decl := &apibsky.SystemDeclaration{
		ActorType: actortype,
	}
	dc, err := r.PutRecord(ctx, "app.bsky.system.declaration/self", decl)
	if err != nil {
		return fmt.Errorf("setting initial actor profile: %w", err)
	}

	if dc.String() != declcid {
		fmt.Println("DECL CID MISMATCH: ", dc, declcid)
	}

	// TODO: set declaration?

	root, err := r.Commit(ctx)
	if err != nil {
		return err
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
					DeclRefCid:  declcid,
					Type:        actortype,
				},
			}},
			RepoSlice: rslice,
		})
	}

	return nil
}

func (rm *RepoManager) GetRepoRoot(ctx context.Context, user uint) (cid.Cid, error) {
	unlock := rm.lockUser(ctx, user)
	defer unlock()

	return rm.getUserRepoHead(ctx, user)
}

func (rm *RepoManager) ReadRepo(ctx context.Context, user uint, fromcid cid.Cid, w io.Writer) error {
	return rm.cs.ReadUserCar(ctx, user, fromcid, true, w)
}

func (rm *RepoManager) GetRecord(ctx context.Context, user uint, collection string, rkey string, maybeCid cid.Cid) (cid.Cid, cbg.CBORMarshaler, error) {
	bs, err := rm.cs.ReadOnlySession(user)
	if err != nil {
		return cid.Undef, nil, err
	}

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return cid.Undef, nil, err
	}

	r, err := repo.OpenRepo(ctx, bs, head)
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

func (rm *RepoManager) GetProfile(ctx context.Context, uid uint) (*apibsky.ActorProfile, error) {
	bs, err := rm.cs.ReadOnlySession(uid)
	if err != nil {
		return nil, err
	}

	head, err := rm.getUserRepoHead(ctx, uid)
	if err != nil {
		return nil, err
	}

	r, err := repo.OpenRepo(ctx, bs, head)
	if err != nil {
		return nil, err
	}

	_, val, err := r.GetRecord(ctx, "app.bsky.actor.profile/self")
	if err != nil {
		return nil, err
	}

	ap, ok := val.(*apibsky.ActorProfile)
	if !ok {
		return nil, fmt.Errorf("found wrong type in actor profile location in tree")
	}

	return ap, nil
}

func (rm *RepoManager) HandleExternalUserEvent(ctx context.Context, pdsid uint, uid uint, ops []*events.RepoOp, carslice []byte) error {
	root, ds, err := rm.cs.ImportSlice(ctx, uid, carslice)
	if err != nil {
		return fmt.Errorf("importing external carslice: %w", err)
	}

	r, err := repo.OpenRepo(ctx, ds, root)
	if err != nil {
		return fmt.Errorf("opening external user repo: %w", err)
	}

	log.Infow("external event", "uid", uid, "ops", ops)

	var evtops []RepoOp
	for _, op := range ops {
		switch EventKind(op.Kind) {
		case EvtKindCreateRecord:
			recid, rec, err := r.GetRecord(ctx, op.Col+"/"+op.Rkey)
			if err != nil {
				return fmt.Errorf("reading changed record from car slice: %w", err)
			}

			evtops = append(evtops, RepoOp{
				Kind:       EvtKindCreateRecord,
				Collection: op.Col,
				Rkey:       op.Rkey,
				Record:     rec,
				RecCid:     recid,
			})
		case EvtKindInitActor:
			var ai types.ActorInfo
			if err := rm.db.First(&ai, "id = ?", uid).Error; err != nil {
				return fmt.Errorf("expected initialized user: %w", err)
			}

			evtops = append(evtops, RepoOp{
				Kind: EvtKindInitActor,
				ActorInfo: &ActorInfo{
					Did:         ai.Did,
					Handle:      ai.Handle,
					DisplayName: ai.DisplayName,
					DeclRefCid:  ai.DeclRefCid,
					Type:        ai.Type,
				},
			})
		case EvtKindUpdateRecord:
			recid, rec, err := r.GetRecord(ctx, op.Col+"/"+op.Rkey)
			if err != nil {
				return fmt.Errorf("reading changed record from car slice: %w", err)
			}

			evtops = append(evtops, RepoOp{
				Kind:       EvtKindUpdateRecord,
				Collection: op.Col,
				Rkey:       op.Rkey,
				Record:     rec,
				RecCid:     recid,
			})
		default:
			return fmt.Errorf("unrecognized external user event kind: %q", op.Kind)
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
		fmt.Println("calling out to events handler...")
		rm.events(ctx, &RepoEvent{
			User: uid,
			//OldRoot:    head,
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

/*
func anyRecordParse(rec any) (cbg.CBORMarshaler, error) {
	// TODO: really should just have a fancy type that auto-things upon json unmarshal
	rmap, ok := rec.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("record should have been an object")
	}

	t, ok := rmap["$type"].(string)
	if !ok {
		return nil, fmt.Errorf("records must have string $type field")
	}
}
*/

func (rm *RepoManager) BatchWrite(ctx context.Context, user uint, writes []*atproto.RepoBatchWrite_Input_Writes_Elem) error {
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

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return err
	}

	var ops []RepoOp
	for _, w := range writes {
		switch {
		case w.RepoBatchWrite_Create != nil:
			c := w.RepoBatchWrite_Create
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
				RecCid:     cc,
				Record:     c.Value.Val,
			})
		case w.RepoBatchWrite_Update != nil:
			u := w.RepoBatchWrite_Update

			cc, err := r.PutRecord(ctx, u.Collection+"/"+u.Rkey, u.Value.Val)
			if err != nil {
				return err
			}

			ops = append(ops, RepoOp{
				Kind:       EvtKindUpdateRecord,
				Collection: u.Collection,
				Rkey:       u.Rkey,
				RecCid:     cc,
				Record:     u.Value.Val,
			})
		case w.RepoBatchWrite_Delete != nil:
			d := w.RepoBatchWrite_Delete

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

	nroot, err := r.Commit(ctx)
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

	if rm.events != nil {
		rm.events(ctx, &RepoEvent{
			User:      user,
			OldRoot:   head,
			NewRoot:   nroot,
			RepoSlice: rslice,
			Ops:       ops,
		})
	}

	return nil
}
