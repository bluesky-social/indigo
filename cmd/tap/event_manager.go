package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/cmd/tap/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EventManager struct {
	logger *slog.Logger
	db     *gorm.DB

	nextID atomic.Uint64

	cacheSize       int
	finishedLoading atomic.Bool

	cache   map[uint]*OutboxEvt
	cacheLk sync.RWMutex

	pendingIDs chan uint
}

func NewEventManager(logger *slog.Logger, db *gorm.DB, config *TapConfig) *EventManager {
	return &EventManager{
		logger:     logger.With("component", "event_manager"),
		db:         db,
		cacheSize:  config.EventCacheSize,
		cache:      make(map[uint]*OutboxEvt),
		pendingIDs: make(chan uint, config.EventCacheSize*2), // give us some buffer room in channel since we can overshoot
	}
}

type DBCallback = func(tx *gorm.DB) error

func (em *EventManager) IsFull() bool {
	em.cacheLk.RLock()
	defer em.cacheLk.RUnlock()
	return len(em.cache) >= em.cacheSize
}

func (em *EventManager) IsReady() bool {
	return em.finishedLoading.Load() && !em.IsFull()
}

func (em *EventManager) WaitForReady(ctx context.Context) {
	if em.IsReady() {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if em.IsReady() {
				return
			}
		}
	}
}

func (em *EventManager) GetEvent(id uint) (*OutboxEvt, bool) {
	em.cacheLk.RLock()
	defer em.cacheLk.RUnlock()
	evt, exists := em.cache[id]
	return evt, exists
}

func (em *EventManager) DeleteEvents(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}

	if err := em.db.WithContext(ctx).Delete(&models.OutboxBuffer{}, ids).Error; err != nil {
		return err
	}

	em.cacheLk.Lock()
	defer em.cacheLk.Unlock()
	for _, id := range ids {
		delete(em.cache, id)
	}
	eventCacheSize.Set(float64(len(em.cache)))
	return nil
}

func (em *EventManager) LoadEvents(ctx context.Context) {
	lastID := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if em.IsFull() {
				time.Sleep(10 * time.Millisecond)
				continue
			}

			lastPageID, err := em.loadEventPage(ctx, lastID)
			if err != nil {
				em.logger.Error("failed to load events into cache", "error", err, "lastID", lastID)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if lastPageID == lastID {
				em.finishedLoading.Store(true)
				return
			}
			lastID = lastPageID
		}
	}
}

func (em *EventManager) loadEventPage(ctx context.Context, lastID int) (int, error) {
	batchSize := 1000
	dbEvts := make([]models.OutboxBuffer, 0, batchSize)
	if err := em.db.WithContext(ctx).Raw(
		"SELECT * FROM outbox_buffers WHERE id > ? ORDER BY id ASC LIMIT ?",
		lastID,
		batchSize,
	).Scan(&dbEvts).Error; err != nil {
		return 0, err
	}

	resultSize := len(dbEvts)
	if resultSize == 0 {
		return lastID, nil
	}

	em.cacheLk.Lock()
	for i := range dbEvts {
		entry := &OutboxEvt{
			ID:    dbEvts[i].ID,
			Did:   dbEvts[i].Did,
			Live:  dbEvts[i].Live,
			Event: []byte(dbEvts[i].Data),
		}
		em.cache[entry.ID] = entry
	}
	eventCacheSize.Set(float64(len(em.cache)))
	em.cacheLk.Unlock()

	maxID := dbEvts[len(dbEvts)-1].ID
	em.nextID.Store(uint64(maxID + 1))

	for i := range dbEvts {
		em.pendingIDs <- dbEvts[i].ID
	}

	return int(dbEvts[resultSize-1].ID), nil
}

func (em *EventManager) AddCommit(ctx context.Context, commit *Commit, dbCallback DBCallback) error {
	if len(commit.Ops) == 0 {
		return updateRepoCommitMeta(em.db, commit)
	}

	evts := make([]*RecordEvt, 0, len(commit.Ops))

	for _, op := range commit.Ops {
		evt := &RecordEvt{
			Live:       true,
			Did:        commit.Did,
			Rev:        commit.Rev,
			Collection: op.Collection,
			Rkey:       op.Rkey,
			Action:     op.Action,
			Record:     op.Record,
			Cid:        op.Cid,
		}
		evts = append(evts, evt)
	}

	return em.AddRecordEvents(ctx, evts, true, func(tx *gorm.DB) error {
		if err := dbCallback(tx); err != nil {
			return err
		}

		return updateRepoCommitMeta(tx, commit)
	})
}

func updateRepoCommitMeta(dbOrTx *gorm.DB, commit *Commit) error {
	return dbOrTx.Model(&models.Repo{}).
		Where("did = ?", commit.Did).
		Updates(map[string]interface{}{
			"rev":       commit.Rev,
			"prev_data": commit.DataCid,
		}).Error
}

func (em *EventManager) AddRecordEvents(ctx context.Context, evts []*RecordEvt, live bool, dbCallback DBCallback) error {
	toPut := make([]*models.RepoRecord, 0, len(evts))
	toDel := make([]*models.RepoRecord, 0)
	dbEvts := make([]*models.OutboxBuffer, 0, len(evts))
	cacheEvts := make(map[uint]OutboxEvt, len(evts))
	evtIDs := make([]uint, 0, len(evts))

	for _, evt := range evts {
		if evt.Action == "delete" {
			toDel = append(toDel, &models.RepoRecord{
				Did:        evt.Did,
				Collection: evt.Collection,
				Rkey:       evt.Rkey,
			})
		} else {
			toPut = append(toPut, &models.RepoRecord{
				Did:        evt.Did,
				Collection: evt.Collection,
				Rkey:       evt.Rkey,
				Cid:        evt.Cid,
			})
		}

		evtID := uint(em.nextID.Add(1))
		evtIDs = append(evtIDs, evtID)

		jsonData, err := evt.MarshalWithId(evtID)
		if err != nil {
			return err
		}

		dbEvts = append(dbEvts, &models.OutboxBuffer{
			ID:   evtID,
			Did:  evt.Did,
			Live: live,
			Data: string(jsonData),
		})

		cacheEvts[evtID] = OutboxEvt{
			ID:    evtID,
			Did:   evt.Did,
			Live:  live,
			Event: jsonData,
		}
	}

	err := em.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dbCallback(tx); err != nil {
			return err
		}

		if len(toDel) > 0 {
			for _, op := range toDel {
				if err := tx.Delete(&models.RepoRecord{}, "did = ? AND collection = ? AND rkey = ?",
					op.Did, op.Collection, op.Rkey).Error; err != nil {
					return err
				}
			}
		}

		if len(toPut) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "did"}, {Name: "collection"}, {Name: "rkey"}},
				DoUpdates: clause.AssignmentColumns([]string{"cid"}),
			}).CreateInBatches(toPut, 100).Error; err != nil {
				return err
			}
		}

		if len(dbEvts) > 0 {
			if err := tx.CreateInBatches(dbEvts, 100).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	em.cacheLk.Lock()
	for id, entry := range cacheEvts {
		entryCopy := entry // Create heap copy
		em.cache[id] = &entryCopy
	}
	eventCacheSize.Set(float64(len(em.cache)))
	em.cacheLk.Unlock()

	for _, evtID := range evtIDs {
		em.pendingIDs <- evtID
	}

	return nil
}

func (em *EventManager) AddIdentityEvent(ctx context.Context, evt *IdentityEvt, dbCallback DBCallback) error {
	evtID := uint(em.nextID.Add(1))
	jsonData, err := evt.MarshalWithId(evtID)
	if err != nil {
		return err
	}

	if err := em.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dbCallback(tx); err != nil {
			return err
		}
		return tx.Create(&models.OutboxBuffer{
			ID:   evtID,
			Did:  evt.Did,
			Live: false,
			Data: string(jsonData),
		}).Error
	}); err != nil {
		return err
	}

	em.cacheLk.Lock()
	em.cache[evtID] = &OutboxEvt{
		ID:    evtID,
		Did:   evt.Did,
		Live:  false,
		Event: jsonData,
	}
	eventCacheSize.Set(float64(len(em.cache)))
	em.cacheLk.Unlock()

	em.pendingIDs <- evtID

	return nil
}

func (em *EventManager) addToResyncBuffer(ctx context.Context, commit *Commit) error {
	jsonData, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	return em.db.WithContext(ctx).Create(&models.ResyncBuffer{
		Did:  commit.Did,
		Data: string(jsonData),
	}).Error
}
