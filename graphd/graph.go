package graphd

import (
	"fmt"
	"sync"
)

type Graph struct {
	follows   sync.Map
	following sync.Map

	utd   map[uint64]string
	utdLk sync.RWMutex

	dtu   map[string]uint64
	dtuLk sync.RWMutex

	uidNext uint64
	nextLk  sync.Mutex

	followCount   uint64
	followCountLk sync.RWMutex

	userCount   uint64
	userCountLk sync.RWMutex
}

type FollowMap struct {
	data map[uint64]struct{}
	lk   sync.RWMutex
}

func NewGraph() *Graph {
	return &Graph{
		utd: map[uint64]string{},
		dtu: map[string]uint64{},
	}
}

func (g *Graph) GetUsercount() uint64 {
	g.userCountLk.RLock()
	defer g.userCountLk.RUnlock()
	return g.userCount
}

func (g *Graph) GetFollowcount() uint64 {
	g.followCountLk.RLock()
	defer g.followCountLk.RUnlock()
	return g.followCount
}

func (g *Graph) GetDID(uid uint64) (string, bool) {
	g.utdLk.RLock()
	defer g.utdLk.RUnlock()
	did, ok := g.utd[uid]
	return did, ok
}

func (g *Graph) GetDIDs(uids []uint64) ([]string, error) {
	g.utdLk.RLock()
	defer g.utdLk.RUnlock()
	dids := make([]string, len(uids))
	for i, uid := range uids {
		did, ok := g.utd[uid]
		if !ok {
			return nil, fmt.Errorf("uid %d not found", uid)
		}
		dids[i] = did
	}
	return dids, nil
}

func (g *Graph) GetUID(did string) (uint64, bool) {
	g.dtuLk.RLock()
	defer g.dtuLk.RUnlock()
	uid, ok := g.dtu[did]
	return uid, ok
}

func (g *Graph) GetUIDs(dids []string) ([]uint64, error) {
	g.dtuLk.RLock()
	defer g.dtuLk.RUnlock()
	uids := make([]uint64, len(dids))
	for i, did := range dids {
		uid, ok := g.dtu[did]
		if !ok {
			return nil, fmt.Errorf("did %s not found", did)
		}
		uids[i] = uid
	}
	return uids, nil
}

func (g *Graph) setUID(did string, uid uint64) {
	g.dtuLk.Lock()
	defer g.dtuLk.Unlock()
	g.dtu[did] = uid
}

func (g *Graph) nextUID() uint64 {
	g.nextLk.Lock()
	defer g.nextLk.Unlock()
	uid := g.uidNext
	g.uidNext++
	return uid
}

func (g *Graph) setDID(uid uint64, did string) {
	g.utdLk.Lock()
	defer g.utdLk.Unlock()
	g.utd[uid] = did
}

// AcquireDID links a DID to a UID, creating a new UID if necessary.
// If the DID is already linked to a UID, that UID is returned
func (g *Graph) AcquireDID(did string) uint64 {
	uid, ok := g.GetUID(did)
	if !ok {
		uid = g.nextUID()
		g.setUID(did, uid)
		g.setDID(uid, did)

		// Initialize the follow maps
		g.follows.Store(uid, &FollowMap{
			data: map[uint64]struct{}{},
		})
		g.following.Store(uid, &FollowMap{
			data: map[uint64]struct{}{},
		})

		g.userCountLk.Lock()
		g.userCount++
		g.userCountLk.Unlock()
	}
	return uid
}

func (g *Graph) AddFollow(actorUID, targetUID uint64) {
	followMap, ok := g.follows.Load(actorUID)
	if !ok {
		followMap = &FollowMap{
			data: map[uint64]struct{}{},
		}
		g.follows.Store(actorUID, followMap)
	}
	followMap.(*FollowMap).lk.Lock()
	followMap.(*FollowMap).data[targetUID] = struct{}{}
	followMap.(*FollowMap).lk.Unlock()

	followMap, ok = g.following.Load(targetUID)
	if !ok {
		followMap = &FollowMap{
			data: map[uint64]struct{}{},
		}
		g.following.Store(targetUID, followMap)
	}
	followMap.(*FollowMap).lk.Lock()
	followMap.(*FollowMap).data[actorUID] = struct{}{}
	followMap.(*FollowMap).lk.Unlock()

	g.followCountLk.Lock()
	g.followCount++
	g.followCountLk.Unlock()
}

// RemoveFollow removes a follow from the graph if it exists
func (g *Graph) RemoveFollow(actorUID, targetUID uint64) {
	followMap, ok := g.follows.Load(actorUID)
	if ok {
		followMap.(*FollowMap).lk.Lock()
		delete(followMap.(*FollowMap).data, targetUID)
		followMap.(*FollowMap).lk.Unlock()
	}

	followMap, ok = g.following.Load(targetUID)
	if !ok {
		followMap.(*FollowMap).lk.Lock()
		delete(followMap.(*FollowMap).data, actorUID)
		followMap.(*FollowMap).lk.Unlock()
	}
}

func (g *Graph) GetFollowers(uid uint64) ([]uint64, error) {
	followMap, ok := g.following.Load(uid)
	if !ok {
		return nil, fmt.Errorf("uid %d not found", uid)
	}
	followMap.(*FollowMap).lk.RLock()
	defer followMap.(*FollowMap).lk.RUnlock()

	followers := make([]uint64, len(followMap.(*FollowMap).data))
	i := 0
	for follower := range followMap.(*FollowMap).data {
		followers[i] = follower
		i++
	}

	return followers, nil
}

func (g *Graph) GetFollowing(uid uint64) ([]uint64, error) {
	followMap, ok := g.follows.Load(uid)
	if !ok {
		return nil, fmt.Errorf("uid %d not found", uid)
	}
	followMap.(*FollowMap).lk.RLock()
	defer followMap.(*FollowMap).lk.RUnlock()

	following := make([]uint64, len(followMap.(*FollowMap).data))
	i := 0
	for follower := range followMap.(*FollowMap).data {
		following[i] = follower
		i++
	}

	return following, nil
}

func (g *Graph) DoesFollow(actorUID, targetUID uint64) (bool, error) {
	followMap, ok := g.follows.Load(actorUID)
	if !ok {
		return false, fmt.Errorf("actor uid %d not found", actorUID)
	}
	followMap.(*FollowMap).lk.RLock()
	defer followMap.(*FollowMap).lk.RUnlock()

	_, ok = followMap.(*FollowMap).data[targetUID]
	return ok, nil
}
