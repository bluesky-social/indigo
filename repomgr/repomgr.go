package repomgr

import (
	"context"
	"io"
	"sync"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/repo"
	"gorm.io/gorm"
)

func NewRepoManager(db *gorm.DB, cs *carstore.CarStore, cb func(*RepoEvent)) *RepoManager {
	db.AutoMigrate(RepoHead{})

	return &RepoManager{
		db:        db,
		cs:        cs,
		events:    cb,
		userLocks: make(map[uint]*userLock),
	}
}

type RepoManager struct {
	cs *carstore.CarStore
	db *gorm.DB

	lklk      sync.Mutex
	userLocks map[uint]*userLock

	events func(*RepoEvent)
}

type ActorInfo struct {
	Did         string
	Handle      string
	DisplayName string
	DeclRefCid  string
	Type        string
}

type RepoEvent struct {
	Kind       string
	User       uint
	OldRoot    cid.Cid
	NewRoot    cid.Cid
	Collection string
	Rkey       string
	RecCid     cid.Cid
	Record     any
	ActorInfo  *ActorInfo
}

type RepoHead struct {
	gorm.Model
	User uint
	Root string
}

type userLock struct {
	lk    sync.Mutex
	count int
}

func (rm *RepoManager) lockUser(user uint) func() {
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
	var headrec RepoHead
	if err := rm.db.First(&headrec, "user = ?", user).Error; err != nil {
		return cid.Undef, err
	}

	cc, err := cid.Decode(headrec.Root)
	if err != nil {
		return cid.Undef, err
	}

	return cc, nil
}

func (rm *RepoManager) updateUserRepoHead(ctx context.Context, user uint, root cid.Cid) error {
	if err := rm.db.Model(RepoHead{}).Where("user = ?", user).Update("root", root.String()).Error; err != nil {
		return err
	}

	return nil
}

func (rm *RepoManager) CreateRecord(ctx context.Context, user uint, collection string, rec cbg.CBORMarshaler) (string, cid.Cid, error) {
	ntid := repo.NextTID()

	unlock := rm.lockUser(user)
	defer unlock()

	rkey := collection + "/" + ntid

	head, err := rm.getUserRepoHead(ctx, user)
	if err != nil {
		return "", cid.Undef, err
	}

	ds, err := rm.cs.NewDeltaSession(user, head)
	if err != nil {
		return "", cid.Undef, err
	}

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return "", cid.Undef, err
	}

	cc, err := r.CreateRecord(ctx, rkey, rec)
	if err != nil {
		return "", cid.Undef, err
	}

	nroot, err := r.Commit(ctx)
	if err != nil {
		return "", cid.Undef, err
	}

	if err := ds.CloseWithRoot(ctx, nroot); err != nil {
		return "", cid.Undef, err
	}

	// TODO: what happens if this update fails?
	if err := rm.updateUserRepoHead(ctx, user, nroot); err != nil {
		return "", cid.Undef, err
	}

	if rm.events != nil {
		rm.events(&RepoEvent{
			Kind:       "createRecord",
			User:       user,
			OldRoot:    head,
			NewRoot:    nroot,
			Collection: collection,
			Rkey:       rkey,
			Record:     rec,
			RecCid:     cc,
		})
	}

	return rkey, cc, nil
}

func (rm *RepoManager) InitNewActor(ctx context.Context, user uint, handle, did, displayname string, declcid, actortype string) error {
	unlock := rm.lockUser(user)
	defer unlock()

	ds, err := rm.cs.NewDeltaSession(user, cid.Undef)
	if err != nil {
		return err
	}

	r := repo.NewRepo(ctx, ds)

	// TODO: set displayname?
	// TODO: set declaration?

	root, err := r.Commit(ctx)
	if err != nil {
		return err
	}

	if err := ds.CloseWithRoot(ctx, root); err != nil {
		return err
	}

	if err := rm.db.Create(&RepoHead{
		User: user,
		Root: root.String(),
	}).Error; err != nil {
		return err
	}

	if rm.events != nil {
		rm.events(&RepoEvent{
			Kind:    "initActor",
			User:    user,
			NewRoot: root,
			ActorInfo: &ActorInfo{
				Did:         did,
				Handle:      handle,
				DisplayName: displayname,
				DeclRefCid:  declcid,
				Type:        actortype,
			},
		})
	}

	return nil
}

func (rm *RepoManager) GetRepoRoot(ctx context.Context, user uint) (cid.Cid, error) {
	unlock := rm.lockUser(user)
	defer unlock()

	return rm.getUserRepoHead(ctx, user)
}

func (rm *RepoManager) ReadRepo(ctx context.Context, user uint, fromcid cid.Cid, w io.Writer) error {
	return rm.cs.ReadUserCar(ctx, user, fromcid, true, w)
}
