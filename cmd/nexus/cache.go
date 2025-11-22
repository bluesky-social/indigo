package main

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"gorm.io/gorm"
)

type EventThing struct {
	DB *gorm.DB

	nextID atomic.Uint64

	eventCache map[uint]*CacheEntry
	cacheMu    sync.RWMutex
}

type CacheEntry struct {
	Event []byte
	Did   string
}

func (em *EventThing) AddOutboxEvents(evts []OutboxEvt, live bool) error {
	entries, err := em.AddOutboxEventsTxOnly(em.DB, evts, live)
	if err != nil {
		return err
	}
	em.AddCacheEntries(entries)
	return nil
}

func (em *EventThing) AddOutboxEventsTxOnly(tx *gorm.DB, evts []OutboxEvt, live bool) (map[uint]CacheEntry, error) {
	dbEvts := make([]*models.OutboxBuffer, 0, len(evts))
	cacheEntries := make(map[uint]CacheEntry, len(evts))

	if len(evts) == 0 {
		return cacheEntries, nil
	}

	for _, evt := range evts {
		evtID := uint(em.nextID.Add(1))

		jsonData, err := json.Marshal(&evt)
		if err != nil {
			return nil, err
		}

		dbEvts = append(dbEvts, &models.OutboxBuffer{
			ID:   evtID,
			Live: live,
			Data: string(jsonData),
		})

		cacheEntries[evtID] = CacheEntry{
			Event: jsonData,
			Did:   evt.DID(),
		}
	}

	if err := tx.CreateInBatches(dbEvts, 100).Error; err != nil {
		return nil, err
	}

	return cacheEntries, nil
}

func (em *EventThing) AddCacheEntries(entries map[uint]CacheEntry) {
	em.cacheMu.Lock()
	for id, entry := range entries {
		entryCopy := entry // Create heap copy
		em.eventCache[id] = &entryCopy
	}
	em.cacheMu.Unlock()
}

func (em *EventThing) DeleteEvents(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}

	if err := em.DB.Delete(&models.OutboxBuffer{}, ids).Error; err != nil {
		return err
	}

	em.cacheMu.Lock()
	defer em.cacheMu.Unlock()
	for _, id := range ids {
		delete(em.eventCache, id)
	}
	return nil
}
