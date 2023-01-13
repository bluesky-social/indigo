package repomgr

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	atproto "github.com/whyrusleeping/gosky/api/atproto"
	apibsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/repo"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
	Kind       EventKind
	User       uint
	OldRoot    cid.Cid
	NewRoot    cid.Cid
	Collection string
	Rkey       string
	RecCid     cid.Cid
	Record     any
	ActorInfo  *ActorInfo
	RepoSlice  []byte
	PDS        uint
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
			Kind:       EvtKindCreateRecord,
			User:       user,
			OldRoot:    head,
			NewRoot:    nroot,
			Collection: collection,
			Rkey:       tid,
			Record:     rec,
			RecCid:     cc,
			RepoSlice:  rslice,
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
			Kind:       EvtKindUpdateRecord,
			User:       user,
			OldRoot:    head,
			NewRoot:    nroot,
			Collection: collection,
			Rkey:       rkey,
			Record:     rec,
			RecCid:     cc,
			RepoSlice:  rslice,
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
			Kind:       EvtKindDeleteRecord,
			User:       user,
			OldRoot:    head,
			NewRoot:    nroot,
			Collection: collection,
			Rkey:       rkey,
			RepoSlice:  rslice,
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
			Kind:    EvtKindInitActor,
			User:    user,
			NewRoot: root,
			ActorInfo: &ActorInfo{
				Did:         did,
				Handle:      handle,
				DisplayName: displayname,
				DeclRefCid:  declcid,
				Type:        actortype,
			},
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

func (rm *RepoManager) GetRecord(ctx context.Context, user uint, collection string, rkey string, maybeCid cid.Cid) (cid.Cid, any, error) {
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

func (rm *RepoManager) HandleExternalUserEvent(ctx context.Context, pdsid uint, kind EventKind, uid uint, collection string, rkey string, carslice []byte) error {
	root, ds, err := rm.cs.ImportSlice(ctx, uid, carslice)
	if err != nil {
		return fmt.Errorf("importing external carslice: %w", err)
	}

	r, err := repo.OpenRepo(ctx, ds, root)
	if err != nil {
		return fmt.Errorf("opening external user repo: %w", err)
	}

	switch kind {
	case EvtKindCreateRecord:
		recid, rec, err := r.GetRecord(ctx, collection+"/"+rkey)
		if err != nil {
			return fmt.Errorf("reading changed record from car slice: %w", err)
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
				Kind: EvtKindCreateRecord,
				User: uid,
				//OldRoot:    head,
				NewRoot:    root,
				Collection: collection,
				Rkey:       rkey,
				Record:     rec,
				RecCid:     recid,
				RepoSlice:  rslice,
				PDS:        pdsid,
			})
		}
		return nil
	default:
		return fmt.Errorf("unrecognized external user event kind: %q", kind)
	}

	return nil
}

func nsidForCollection(collection string) string {
	return collection + "/" + repo.NextTID()
}

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

	for _, w := range writes {
		switch {
		case w.RepoBatchWrite_Create != nil:
			c := w.RepoBatchWrite_Create
			var nsid string
			if c.Rkey != nil {
				nsid = c.Collection + "/" + *c.Rkey
			} else {
				nsid = nsidForCollection(c.Collection)
			}

			cc, rpath, err := r.CreateRecord(ctx, nsid, c.Value)
			if err != nil {
				return err
			}

			_ = rpath
			_ = cc // do we do something about this?
		case w.RepoBatchWrite_Update != nil:
			u := w.RepoBatchWrite_Update

			cc, err := r.PutRecord(ctx, u.Collection+"/"+u.Rkey, u.Value)
			if err != nil {
				return err
			}

			_ = cc
		case w.RepoBatchWrite_Delete != nil:
			d := w.RepoBatchWrite_Delete

			if err := r.DeleteRecord(ctx, d.Collection+"/"+d.Rkey); err != nil {
				return err
			}
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
			Kind:       EvtKindDeleteRecord,
			User:       user,
			OldRoot:    head,
			NewRoot:    nroot,
			Collection: collection,
			Rkey:       rkey,
			RepoSlice:  rslice,
		})
	}

	return nil
}
