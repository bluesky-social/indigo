package repomgr

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/models"

	"github.com/ipfs/go-cid"
)

type MemHeadStore struct {
	heads map[models.Uid]cid.Cid
	lk    sync.RWMutex
}

func NewMemHeadStore() *MemHeadStore {
	return &MemHeadStore{
		heads: make(map[models.Uid]cid.Cid),
	}
}

func (hs *MemHeadStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	hs.lk.RLock()
	defer hs.lk.RUnlock()
	h, ok := hs.heads[user]
	if !ok {
		return cid.Undef, fmt.Errorf("user head not found")
	}

	return h, nil
}

func (hs *MemHeadStore) UpdateUserRepoHead(ctx context.Context, user models.Uid, root cid.Cid) error {
	hs.lk.Lock()
	defer hs.lk.Unlock()
	_, ok := hs.heads[user]
	if !ok {
		return fmt.Errorf("cannot update user head if it doesnt exist already")
	}

	hs.heads[user] = root
	return nil
}

func (hs *MemHeadStore) InitUser(ctx context.Context, user models.Uid, root cid.Cid) error {
	hs.lk.Lock()
	defer hs.lk.Unlock()
	_, ok := hs.heads[user]
	if ok {
		return fmt.Errorf("cannot init user head if it exists already")
	}

	hs.heads[user] = root
	return nil
}
