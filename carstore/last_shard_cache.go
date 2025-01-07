package carstore

import (
	"context"
	"github.com/bluesky-social/indigo/models"
	"go.opentelemetry.io/otel"
	"sync"
)

type LastShardSource interface {
	GetLastShard(context.Context, models.Uid) (*CarShard, error)
}

type lastShardCache struct {
	source LastShardSource

	lscLk          sync.Mutex
	lastShardCache map[models.Uid]*CarShard
}

func (lsc *lastShardCache) Init() {
	lsc.lastShardCache = make(map[models.Uid]*CarShard)
}

func (lsc *lastShardCache) check(user models.Uid) *CarShard {
	lsc.lscLk.Lock()
	defer lsc.lscLk.Unlock()

	ls, ok := lsc.lastShardCache[user]
	if ok {
		return ls
	}

	return nil
}

func (lsc *lastShardCache) remove(user models.Uid) {
	lsc.lscLk.Lock()
	defer lsc.lscLk.Unlock()

	delete(lsc.lastShardCache, user)
}

func (lsc *lastShardCache) put(ls *CarShard) {
	if ls == nil {
		return
	}
	lsc.lscLk.Lock()
	defer lsc.lscLk.Unlock()

	lsc.lastShardCache[ls.Usr] = ls
}

func (lsc *lastShardCache) get(ctx context.Context, user models.Uid) (*CarShard, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "getLastShard")
	defer span.End()

	maybeLs := lsc.check(user)
	if maybeLs != nil {
		return maybeLs, nil
	}

	lastShard, err := lsc.source.GetLastShard(ctx, user)
	if err != nil {
		return nil, err
	}

	lsc.put(lastShard)
	return lastShard, nil
}
