package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/cockroachdb/pebble"
	"log/slog"
	"sync"
	"time"
)

func makeCollectionInternKey(collection string) []byte {
	out := make([]byte, len(collection)+1)
	out[0] = 'C'
	copy(out[1:], collection)
	return out
}

func parseCollectionInternKey(key []byte) string {
	if key[0] != 'C' {
		panic(fmt.Sprintf("collection key must start with C, got %v", key[0]))
	}
	return string(key[1:])
}

func makePrimaryPebbleRow(collectionId uint32, did string, seenMs int64) []byte {
	out := make([]byte, 1+4+8+len(did))
	out[0] = 'A'
	binary.BigEndian.PutUint32(out[1:], collectionId)
	pos := 1 + 4
	binary.BigEndian.PutUint64(out[pos:], uint64(seenMs))
	pos += 8
	copy(out[pos:], did)
	return out
}

func parsePrimaryPebbleRow(row []byte) (collectionId uint32, did string, seenMs int64) {
	if row[0] != 'A' {
		panic(fmt.Sprintf("primary row key wanted A got %v", row[0]))
	}
	collectionId = binary.BigEndian.Uint32(row[1:5])
	seenMs = int64(binary.BigEndian.Uint64(row[5:13]))
	did = string(row[13:])
	return collectionId, did, seenMs
}

func makeByDidKey(did string, collectionId uint32) []byte {
	out := make([]byte, 1+len(did)+4)
	out[0] = 'D'
	copy(out[1:1+len(did)], did)
	pos := 1 + len(did)
	binary.BigEndian.PutUint32(out[pos:], collectionId)
	return out
}

func parseByDidKey(key []byte) (did string, collectionId uint32) {
	if key[0] != 'D' {
		panic(fmt.Sprintf("by did key wanted D got %v", key[0]))
	}
	last4 := len(key) - 5
	collectionId = binary.BigEndian.Uint32(key[last4:])
	did = string(key[1 : last4+1])
	return did, collectionId
}

// PebbleCollectionDirectory holds a DID<=>{collections} directory in pebble db.
// The primary database is (collection, seen time int64 milliseconds, did)
// Inner schema:
// C{collection} : {uint32 collectionId}
// D{did}{uint32 collectionId} : {uint64 seen ms}
// A{uint32 collectionId}{uint64 seen ms}{did} : 't'
type PebbleCollectionDirectory struct {
	db *pebble.DB

	// collections can be LRU cache if it ever becomes too big
	collections     map[string]uint32
	collectionNames map[uint32]string // TODO: B-tree would be nice
	maxCollectionId uint32
	collectionsLock sync.Mutex

	log *slog.Logger
}

func (pcd *PebbleCollectionDirectory) Open(pebblePath string) error {
	db, err := pebble.Open(pebblePath, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("%s: could not open db, %w", pebblePath, err)
	}
	pcd.db = db
	pcd.collections = make(map[string]uint32)
	pcd.collectionNames = make(map[uint32]string)
	if pcd.log == nil {
		pcd.log = slog.Default()
	}
	return pcd.readAllCollectionInterns(context.Background())
}

func (pcd *PebbleCollectionDirectory) Close() error {
	err := pcd.db.Flush()
	if err != nil {
		pcd.log.Error("pebble flush", "err", err)
	}
	err = pcd.db.Close()
	if err != nil {
		pcd.log.Error("pebble close", "err", err)
	}
	return err
}

// readAllCollectionInterns should only be run at setup time inside Open() when locking against threads is not needed
func (pcd *PebbleCollectionDirectory) readAllCollectionInterns(ctx context.Context) error {
	lower := []byte{'C'}
	upper := []byte{'D'}
	iter, err := pcd.db.NewIterWithContext(ctx, &pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	if err != nil {
		return fmt.Errorf("collection iter start, %w", err)
	}
	defer iter.Close()
	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value, err := iter.ValueAndErr()
		if err != nil {
			return fmt.Errorf("collection iter, %w", err)
		}
		collection := parseCollectionInternKey(key)
		collectionId := binary.BigEndian.Uint32(value)
		count++
		pcd.collections[collection] = collectionId
		pcd.collectionNames[collectionId] = collection
		if collectionId > pcd.maxCollectionId {
			pcd.maxCollectionId = collectionId
		}
		pcd.log.Debug("collection", "name", collection, "id", collectionId)
	}
	pcd.log.Debug("read collections", "count", count, "max", pcd.maxCollectionId)
	return nil
}

type CollectionDidTime struct {
	Collection string
	Did        string
	UnixMillis int64
}

func (pcd *PebbleCollectionDirectory) ReadAllPrimary(ctx context.Context, out chan<- CollectionDidTime) error {
	defer close(out)
	lower := []byte{'A'}
	upper := []byte{'B'}
	iter, err := pcd.db.NewIterWithContext(ctx, &pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	if err != nil {
		return fmt.Errorf("collection iter start, %w", err)
	}
	defer iter.Close()
	count := 0
	done := ctx.Done()
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		collectionId, did, seenMs := parsePrimaryPebbleRow(key)
		count++
		collection := pcd.collectionNames[collectionId]
		rec := CollectionDidTime{
			Collection: collection,
			Did:        did,
			UnixMillis: seenMs,
		}
		select {
		case <-done:
			return nil
		case out <- rec:
		}
	}
	pcd.log.Debug("read primary", "count", count)
	return nil
}

func (pcd *PebbleCollectionDirectory) ReadCollection(ctx context.Context, collection, cursor string, limit int) (result []CollectionDidTime, nextCursor string, err error) {
	var lower []byte
	collectionId, err := pcd.CollectionToId(collection, false)
	if err != nil {
		if err == ErrNotFound {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("collection id err, %w", err)
	}
	if cursor != "" {
		lower, err = base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("could not decode cursor, %w", err)
		}
	} else {
		lower = make([]byte, 1+4)
		lower[0] = 'A'
		binary.BigEndian.PutUint32(lower[1:], collectionId)
	}
	var upper [5]byte
	upper[0] = 'A'
	binary.BigEndian.PutUint32(upper[1:], collectionId+1)
	iter, err := pcd.db.NewIterWithContext(ctx, &pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper[:],
	})
	if err != nil {
		return nil, "", fmt.Errorf("collection iter start, %w", err)
	}
	defer iter.Close()
	count := 0
	done := ctx.Done()
	result = make([]CollectionDidTime, 0, limit)
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		collectionId, did, seenMs := parsePrimaryPebbleRow(key)
		count++
		collection := pcd.collectionNames[collectionId]
		rec := CollectionDidTime{
			Collection: collection,
			Did:        did,
			UnixMillis: seenMs,
		}
		result = append(result, rec)
		breaker := false
		if count >= limit {
			breaker = true
		} else {
			select {
			case <-done:
				breaker = true
			default:
			}
		}
		if breaker {
			prevKey := make([]byte, len(key), len(key)+1)
			copy(prevKey, key)
			prevKey = append(prevKey, 0)
			nextCursor = base64.StdEncoding.EncodeToString(prevKey)
			break
		}
	}
	pcd.log.Debug("read primary", "count", count)
	return result, nextCursor, nil
}

var ErrNotFound = errors.New("not found")

func (pcd *PebbleCollectionDirectory) CollectionToId(collection string, create bool) (uint32, error) {
	pcd.collectionsLock.Lock()
	defer pcd.collectionsLock.Unlock()
	// easy mode: in cache
	collectionId, ok := pcd.collections[collection]
	if ok {
		return collectionId, nil
	}

	// read from db
	key := makeCollectionInternKey(collection)
	value, closer, err := pcd.db.Get(key)
	if closer != nil {
		defer closer.Close()
	}
	if err == nil {
		collectionId = binary.BigEndian.Uint32(value)
		return collectionId, nil
	}

	if !create {
		return 0, ErrNotFound
	}
	// make new id, write to db
	if errors.Is(err, pebble.ErrNotFound) {
		// ok, fall through
	} else if err != nil {
		return 0, fmt.Errorf("pebble get err, %w", err)
	}
	collectionId = pcd.maxCollectionId + 1
	pcd.maxCollectionId = collectionId
	var cib [4]byte
	binary.BigEndian.PutUint32(cib[:], collectionId)
	err = pcd.db.Set(key, cib[:], pebble.NoSync)
	if err != nil {
		return 0, fmt.Errorf("pebble set err, %w", err)
	}
	pcd.collections[collection] = collectionId
	pcd.collectionNames[collectionId] = collection
	return collectionId, nil
}

var trueValue = [1]byte{'t'}

func (pcd *PebbleCollectionDirectory) CountDidCollections(did string) (int, error) {
	lower := make([]byte, 1+len(did))
	lower[0] = 'D'
	copy(lower[1:1+len(did)], did)
	upper := make([]byte, len(lower))
	copy(upper, lower)
	upper[len(upper)-1]++
	ctx := context.Background()
	iter, err := pcd.db.NewIterWithContext(ctx, &pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	if err != nil {
		return 0, fmt.Errorf("did iter start, %w", err)
	}
	defer iter.Close()
	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		//key := iter.Key()
		//xdid, xcollectionId := parseByDidKey(key)
		count++
	}
	return count, nil
}

func (pcd *PebbleCollectionDirectory) MaybeSetCollection(did, collection string) error {
	collectionId, err := pcd.CollectionToId(collection, true)
	if err != nil {
		return err
	}
	dkey := makeByDidKey(did, collectionId)
	_, closer, err := pcd.db.Get(dkey)
	if closer != nil {
		defer closer.Close()
	}
	if err == nil {
		// already exists, done
		pebbleDup.Inc()
		return nil
	}
	if errors.Is(err, pebble.ErrNotFound) {
		// ok, fall through
	} else if err != nil {
		return fmt.Errorf("pebble get err, %w", err)
	}

	now := time.Now()
	pkey := makePrimaryPebbleRow(collectionId, did, now.UnixMilli())
	err = pcd.db.Set(pkey, trueValue[:], pebble.NoSync)
	if err != nil {
		return fmt.Errorf("pebble set err, %w", err)
	}
	var timebytes [8]byte
	binary.BigEndian.PutUint64(timebytes[:], uint64(now.UnixMilli()))
	err = pcd.db.Set(dkey, timebytes[:], pebble.NoSync)
	if err != nil {
		return fmt.Errorf("pebble set err, %w", err)
	}
	pebbleNew.Inc()
	return nil
}

func (pcd *PebbleCollectionDirectory) SetFromResults(results <-chan DidCollection) {
	errcount := 0
	for result := range results {
		err := pcd.MaybeSetCollection(result.Did, result.Collection)
		if err != nil {
			errcount++
			pcd.log.Error("set collection", "err", err)
			if errcount > 0 {
				// TODO: signal backpressure and shutdown
				return
			}
		} else {
			errcount = 0
		}
	}
}

type CollectionStats struct {
	CollectionCounts map[string]uint64 `json:"collections"`
}

func (pcd *PebbleCollectionDirectory) GetCollectionStats() (stats CollectionStats, err error) {
	ctx := context.Background()
	records := make(chan CollectionDidTime, 1000)
	go pcd.ReadAllPrimary(ctx, records)

	stats.CollectionCounts = make(map[string]uint64)

	for rec := range records {
		stats.CollectionCounts[rec.Collection]++
	}

	return stats, nil
}

const seqKey = "Xseq"

func (pcd *PebbleCollectionDirectory) SetSequence(seq int64) error {
	var seqb [8]byte
	binary.BigEndian.PutUint64(seqb[:], uint64(seq))
	return pcd.db.Set([]byte(seqKey), seqb[:], pebble.NoSync)
}
func (pcd *PebbleCollectionDirectory) GetSequence() (int64, bool, error) {
	vbytes, closer, err := pcd.db.Get([]byte(seqKey))
	if closer != nil {
		defer closer.Close()
	}
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("pebble seq err, %w", err)
	}
	seq := int64(binary.BigEndian.Uint64(vbytes))
	return seq, true, nil
}
