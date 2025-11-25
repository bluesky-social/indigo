package main

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EventManager struct {
	logger *slog.Logger
	db     *gorm.DB

	nextID atomic.Uint64

	cache   map[uint]*OutboxEvt
	cacheLk sync.RWMutex

	pendingIDs chan uint
}

type DBCallback = func(tx *gorm.DB) error

func (em *EventManager) GetEvent(id uint) (*OutboxEvt, bool) {
	em.cacheLk.RLock()
	defer em.cacheLk.RUnlock()
	evt, exists := em.cache[id]
	return evt, exists
}

func (em *EventManager) DeleteEvents(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}

	if err := em.db.Delete(&models.OutboxBuffer{}, ids).Error; err != nil {
		return err
	}

	em.cacheLk.Lock()
	defer em.cacheLk.Unlock()
	for _, id := range ids {
		delete(em.cache, id)
	}
	return nil
}

func (em *EventManager) LoadEvents(ctx context.Context) {
	lastID := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			lastPageID, err := em.loadEventPage(lastID)
			if err != nil {
				em.logger.Error("failed to load events into cache", "error", err, "lastID", lastID)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			// finished loading events
			if lastPageID == lastID {
				return
			}
			lastID = lastPageID
		}
	}
}

func (em *EventManager) loadEventPage(lastID int) (int, error) {
	batchSize := 1000
	dbEvts := make([]models.OutboxBuffer, 0, batchSize)
	if err := em.db.Raw(
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
	em.cacheLk.Unlock()

	for i := range dbEvts {
		em.pendingIDs <- dbEvts[i].ID
	}

	return int(dbEvts[resultSize-1].ID), nil
}

func (em *EventManager) AddCommit(commit *Commit, dbCallback DBCallback) error {
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

	return em.AddRecordEvents(evts, true, func(tx *gorm.DB) error {
		if err := dbCallback(tx); err != nil {
			return err
		}

		return tx.Model(&models.Repo{}).
			Where("did = ?", commit.Did).
			Updates(map[string]interface{}{
				"rev":       commit.Rev,
				"prev_data": commit.DataCid,
			}).Error
	})
}

func (em *EventManager) AddRecordEvents(evts []*RecordEvt, live bool, dbCallback DBCallback) error {
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

		jsonData, err := evt.MarshalJSON(evtID)
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

	err := em.db.Transaction(func(tx *gorm.DB) error {
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
	em.cacheLk.Unlock()

	for _, evtID := range evtIDs {
		em.pendingIDs <- evtID
	}

	return nil
}

func (em *EventManager) AddUserEvent(evt *UserEvt, dbCallback DBCallback) error {
	evtID := uint(em.nextID.Add(1))
	jsonData, err := evt.MarshalJSON(evtID)
	if err != nil {
		return err
	}

	if err := em.db.Transaction(func(tx *gorm.DB) error {
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
	em.cacheLk.Unlock()

	em.pendingIDs <- evtID

	return nil
}
