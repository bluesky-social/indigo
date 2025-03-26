package diskpersist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/bluesky-social/indigo/cmd/relay/events"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/models"
	arc "github.com/hashicorp/golang-lru/arc/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	cbg "github.com/whyrusleeping/cbor-gen"
	"gorm.io/gorm"
)

type DiskPersistence struct {
	primaryDir      string
	archiveDir      string
	eventsPerFile   int64
	writeBufferSize int
	retention       time.Duration

	meta *gorm.DB

	broadcast func(*events.XRPCStreamEvent)

	logfi *os.File

	eventCounter int64
	curSeq       int64
	timeSequence bool

	uids     UidSource
	uidCache *arc.ARCCache[models.Uid, string] // TODO: unused
	didCache *arc.ARCCache[string, models.Uid]

	writers *sync.Pool
	buffers *sync.Pool
	scratch []byte

	outbuf *bytes.Buffer
	evtbuf []persistJob

	shutdown chan struct{}

	log *slog.Logger

	lk sync.Mutex

	// takenDownCache is only written with a newly allocated slice; it is always safe to grab a reference to it and use that until done
	// takenDownUpdateLock is held when generating a new slice
	takenDownUpdateLock sync.Mutex
	takenDownCache      atomic.Pointer[map[models.Uid]struct{}]
}

type persistJob struct {
	Bytes  []byte
	Evt    *events.XRPCStreamEvent
	Buffer *bytes.Buffer // so we can put it back in the pool when we're done
}

type jobResult struct {
	Err error
	Seq int64
}

const (
	EvtFlagTakedown = 1 << iota
	EvtFlagRebased
)

var _ (events.EventPersistence) = (*DiskPersistence)(nil)

type DiskPersistOptions struct {
	UIDCacheSize    int
	DIDCacheSize    int
	EventsPerFile   int64
	WriteBufferSize int
	Retention       time.Duration

	Logger *slog.Logger

	TimeSequence bool
}

func DefaultDiskPersistOptions() *DiskPersistOptions {
	return &DiskPersistOptions{
		EventsPerFile:   10_000,
		UIDCacheSize:    1_000_000,
		DIDCacheSize:    1_000_000,
		WriteBufferSize: 50,
		Retention:       time.Hour * 24 * 3, // 3 days
	}
}

type UidSource interface {
	DidToUid(ctx context.Context, did string) (models.Uid, error)
}

func NewDiskPersistence(primaryDir, archiveDir string, db *gorm.DB, opts *DiskPersistOptions) (*DiskPersistence, error) {
	if opts == nil {
		opts = DefaultDiskPersistOptions()
	}

	uidCache, err := arc.NewARC[models.Uid, string](opts.UIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create uid cache: %w", err)
	}

	didCache, err := arc.NewARC[string, models.Uid](opts.DIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create did cache: %w", err)
	}

	err = db.AutoMigrate(&LogFileRef{})
	if err != nil {
		return nil, fmt.Errorf("gorm setup LogFileRef: %w", err)
	}
	err = db.AutoMigrate(&DiskPersistTakedown{})
	if err != nil {
		return nil, fmt.Errorf("gorm setup DiskPersistTakedown: %w", err)
	}

	bufpool := &sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}

	wrpool := &sync.Pool{
		New: func() any {
			return cbg.NewCborWriter(nil)
		},
	}

	dp := &DiskPersistence{
		meta:            db,
		primaryDir:      primaryDir,
		archiveDir:      archiveDir,
		buffers:         bufpool,
		retention:       opts.Retention,
		writers:         wrpool,
		uidCache:        uidCache,
		didCache:        didCache,
		eventsPerFile:   opts.EventsPerFile,
		scratch:         make([]byte, headerSize),
		outbuf:          new(bytes.Buffer),
		writeBufferSize: opts.WriteBufferSize,
		shutdown:        make(chan struct{}),
		timeSequence:    opts.TimeSequence,
		log:             opts.Logger,
	}
	if dp.log == nil {
		dp.log = slog.Default().With("system", "diskpersist")
	}

	if err := dp.resumeLog(); err != nil {
		return nil, err
	}

	go dp.flushRoutine()

	go dp.garbageCollectRoutine()

	return dp, nil
}

type LogFileRef struct {
	gorm.Model
	Path     string
	Archived bool
	SeqStart int64
}

type DiskPersistTakedown struct {
	gorm.Model
	Uid models.Uid `gorm:"unique"`
}

func (dp *DiskPersistence) SetUidSource(uids UidSource) {
	dp.uids = uids
}

func (dp *DiskPersistence) resumeLog() error {
	var lfr LogFileRef
	if err := dp.meta.Order("seq_start desc").Limit(1).Find(&lfr).Error; err != nil {
		return err
	}

	if lfr.ID == 0 {
		// no files, start anew!
		return dp.initLogFile()
	}

	// 0 for the mode is fine since that is only used if O_CREAT is passed
	fi, err := os.OpenFile(filepath.Join(dp.primaryDir, lfr.Path), os.O_RDWR, 0)
	if err != nil {
		return err
	}

	seq, err := scanForLastSeq(fi, -1)
	if err != nil {
		return fmt.Errorf("failed to scan log file for last seqno: %w", err)
	}

	dp.log.Info("loaded seq", "seq", seq, "now", time.Now().UnixMicro(), "time-seq", dp.timeSequence)

	dp.curSeq = seq + 1
	dp.logfi = fi

	return nil
}

func (dp *DiskPersistence) initLogFile() error {
	if err := os.MkdirAll(dp.primaryDir, 0775); err != nil {
		return err
	}

	p := filepath.Join(dp.primaryDir, "evts-0")
	fi, err := os.Create(p)
	if err != nil {
		return err
	}

	if err := dp.meta.Create(&LogFileRef{
		Path:     "evts-0",
		SeqStart: 0,
	}).Error; err != nil {
		return err
	}

	dp.logfi = fi
	dp.curSeq = 1
	return nil
}

// swapLog swaps the current log file out for a new empty one
// must only be called while holding dp.lk
func (dp *DiskPersistence) swapLog(ctx context.Context) error {
	if err := dp.logfi.Close(); err != nil {
		return fmt.Errorf("failed to close current log file: %w", err)
	}

	fname := fmt.Sprintf("evts-%d", dp.curSeq)
	nextp := filepath.Join(dp.primaryDir, fname)

	fi, err := os.Create(nextp)
	if err != nil {
		return err
	}

	if err := dp.meta.Create(&LogFileRef{
		Path:     fname,
		SeqStart: dp.curSeq,
	}).Error; err != nil {
		return err
	}

	dp.logfi = fi
	return nil
}

func scanForLastSeq(fi *os.File, end int64) (int64, error) {
	scratch := make([]byte, headerSize)

	var lastSeq int64 = -1
	var offset int64
	for {
		eh, err := readHeader(fi, scratch)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return lastSeq, nil
			}
			return 0, err
		}

		if end > 0 && eh.Seq > end {
			// return to beginning of offset
			n, err := fi.Seek(offset, io.SeekStart)
			if err != nil {
				return 0, err
			}

			if n != offset {
				return 0, fmt.Errorf("rewind seek failed")
			}

			return eh.Seq, nil
		}

		lastSeq = eh.Seq

		noff, err := fi.Seek(int64(eh.Len), io.SeekCurrent)
		if err != nil {
			return 0, err
		}

		if noff != offset+headerSize+int64(eh.Len) {
			// TODO: must recover from this
			return 0, fmt.Errorf("did not seek to next event properly")
		}

		offset = noff
	}
}

const (
	evtKindCommit    = 1
	evtKindHandle    = 2
	evtKindTombstone = 3
	evtKindIdentity  = 4
	evtKindAccount   = 5
	evtKindSync      = 6
)

var emptyHeader = make([]byte, headerSize)

func (dp *DiskPersistence) addJobToQueue(ctx context.Context, job persistJob) error {
	dp.lk.Lock()
	defer dp.lk.Unlock()

	if err := dp.doPersist(ctx, job); err != nil {
		return err
	}

	// TODO: for some reason replacing this constant with p.writeBufferSize dramatically reduces perf...
	if len(dp.evtbuf) > 400 {
		if err := dp.flushLog(ctx); err != nil {
			return fmt.Errorf("failed to flush disk log: %w", err)
		}
	}

	return nil
}

func (dp *DiskPersistence) flushRoutine() {
	t := time.NewTicker(time.Millisecond * 100)

	for {
		ctx := context.Background()
		select {
		case <-dp.shutdown:
			return
		case <-t.C:
			dp.lk.Lock()
			if err := dp.flushLog(ctx); err != nil {
				// TODO: this happening is quite bad. Need a recovery strategy
				dp.log.Error("failed to flush disk log", "err", err)
			}
			dp.lk.Unlock()
		}
	}
}

func (dp *DiskPersistence) flushLog(ctx context.Context) error {
	if len(dp.evtbuf) == 0 {
		return nil
	}

	_, err := io.Copy(dp.logfi, dp.outbuf)
	if err != nil {
		return err
	}

	dp.outbuf.Truncate(0)

	for _, ej := range dp.evtbuf {
		dp.broadcast(ej.Evt)
		ej.Buffer.Truncate(0)
		dp.buffers.Put(ej.Buffer)
	}

	dp.evtbuf = dp.evtbuf[:0]

	return nil
}

func (dp *DiskPersistence) garbageCollectRoutine() {
	t := time.NewTicker(time.Hour)

	for {
		ctx := context.Background()
		select {
		// Closing a channel can be listened to with multiple routines: https://goplay.tools/snippet/UcwbC0CeJAL
		case <-dp.shutdown:
			return
		case <-t.C:
			if errs := dp.garbageCollect(ctx); len(errs) > 0 {
				for _, err := range errs {
					dp.log.Error("garbage collection error", "err", err)
				}
			}
		}
	}
}

var garbageCollectionsExecuted = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "disk_persister_garbage_collections_executed",
	Help: "Number of garbage collections executed",
}, []string{})

var garbageCollectionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "disk_persister_garbage_collections_errors",
	Help: "Number of errors encountered during garbage collection",
}, []string{})

var refsGarbageCollected = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "disk_persister_garbage_collections_refs_collected",
	Help: "Number of refs collected during garbage collection",
}, []string{})

var filesGarbageCollected = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "disk_persister_garbage_collections_files_collected",
	Help: "Number of files collected during garbage collection",
}, []string{})

func (dp *DiskPersistence) garbageCollect(ctx context.Context) []error {
	garbageCollectionsExecuted.WithLabelValues().Inc()

	// Grab refs created before the retention period
	var refs []LogFileRef
	var errs []error

	defer func() {
		garbageCollectionErrors.WithLabelValues().Add(float64(len(errs)))
	}()

	if err := dp.meta.WithContext(ctx).Find(&refs, "created_at < ?", time.Now().Add(-dp.retention)).Error; err != nil {
		return []error{err}
	}

	oldRefsFound := len(refs)
	refsDeleted := 0
	filesDeleted := 0

	// In the future if we want to support Archiving, we could do that here instead of deleting
	for _, r := range refs {
		dp.lk.Lock()
		currentLogfile := dp.logfi.Name()
		dp.lk.Unlock()

		if filepath.Join(dp.primaryDir, r.Path) == currentLogfile {
			// Don't delete the current log file
			dp.log.Info("skipping deletion of current log file")
			continue
		}

		// Delete the ref in the database to prevent playback from finding it
		if err := dp.meta.WithContext(ctx).Delete(&r).Error; err != nil {
			errs = append(errs, err)
			continue
		}
		refsDeleted++

		// Delete the file from disk
		if err := os.Remove(filepath.Join(dp.primaryDir, r.Path)); err != nil {
			errs = append(errs, err)
			continue
		}
		filesDeleted++
	}

	refsGarbageCollected.WithLabelValues().Add(float64(refsDeleted))
	filesGarbageCollected.WithLabelValues().Add(float64(filesDeleted))

	dp.log.Info("garbage collection complete",
		"filesDeleted", filesDeleted,
		"refsDeleted", refsDeleted,
		"oldRefsFound", oldRefsFound,
	)

	return errs
}

func (dp *DiskPersistence) doPersist(ctx context.Context, pjob persistJob) error {
	seq := dp.curSeq
	if dp.timeSequence {
		seq = time.Now().UnixMicro()
		if seq < dp.curSeq {
			seq = dp.curSeq
		}
		dp.curSeq = seq + 1
	} else {
		dp.curSeq++
	}

	// Set sequence number in event header
	// the rest of the header is set in DiskPersistence.Persist()
	binary.LittleEndian.PutUint64(pjob.Bytes[20:], uint64(seq))

	// update the seq in the message
	// copy the message from outside to a new object, clobber the seq, add it back to the event
	switch {
	case pjob.Evt.RepoCommit != nil:
		pjob.Evt.RepoCommit.Seq = seq
	case pjob.Evt.RepoSync != nil:
		pjob.Evt.RepoSync.Seq = seq
	case pjob.Evt.RepoHandle != nil:
		pjob.Evt.RepoHandle.Seq = seq
	case pjob.Evt.RepoIdentity != nil:
		pjob.Evt.RepoIdentity.Seq = seq
	case pjob.Evt.RepoAccount != nil:
		pjob.Evt.RepoAccount.Seq = seq
	case pjob.Evt.RepoTombstone != nil:
		pjob.Evt.RepoTombstone.Seq = seq
	default:
		// only those three get peristed right now
		// we should not actually ever get here...
		return nil
	}

	_, err := dp.outbuf.Write(pjob.Bytes)
	if err != nil {
		return err
	}

	dp.evtbuf = append(dp.evtbuf, pjob)

	dp.eventCounter++
	if dp.eventCounter%dp.eventsPerFile == 0 {
		if err := dp.flushLog(ctx); err != nil {
			return err
		}

		// time to roll the log file
		if err := dp.swapLog(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Persist implements events.EventPersistence
// Persist may mutate contents of xevt and what it points to
func (dp *DiskPersistence) Persist(ctx context.Context, xevt *events.XRPCStreamEvent) error {
	buffer := dp.buffers.Get().(*bytes.Buffer)
	cw := dp.writers.Get().(*cbg.CborWriter)
	defer dp.writers.Put(cw)
	cw.SetWriter(buffer)

	buffer.Truncate(0)

	buffer.Write(emptyHeader)

	var did string
	var evtKind uint32
	switch {
	case xevt.RepoCommit != nil:
		evtKind = evtKindCommit
		did = xevt.RepoCommit.Repo
		if err := xevt.RepoCommit.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal commit: %w", err)
		}
	case xevt.RepoSync != nil:
		evtKind = evtKindSync
		did = xevt.RepoSync.Did
		if err := xevt.RepoSync.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal sync: %w", err)
		}
	case xevt.RepoHandle != nil:
		evtKind = evtKindHandle
		did = xevt.RepoHandle.Did
		if err := xevt.RepoHandle.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal handle: %w", err)
		}
	case xevt.RepoIdentity != nil:
		evtKind = evtKindIdentity
		did = xevt.RepoIdentity.Did
		if err := xevt.RepoIdentity.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal ident: %w", err)
		}
	case xevt.RepoAccount != nil:
		evtKind = evtKindAccount
		did = xevt.RepoAccount.Did
		if err := xevt.RepoAccount.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal account: %w", err)
		}
	case xevt.RepoTombstone != nil:
		evtKind = evtKindTombstone
		did = xevt.RepoTombstone.Did
		if err := xevt.RepoTombstone.MarshalCBOR(cw); err != nil {
			return fmt.Errorf("failed to marshal tombstone: %w", err)
		}
	default:
		return nil
		// only those two get peristed right now
	}

	usr, err := dp.uidForDid(ctx, did)
	if err != nil {
		return err
	}

	b := buffer.Bytes()

	// Set flags in header (no flags for now)
	binary.LittleEndian.PutUint32(b, 0)
	// Set event kind in header
	binary.LittleEndian.PutUint32(b[4:], evtKind)
	// Set event length in header
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)-headerSize))
	// Set user UID in header
	binary.LittleEndian.PutUint64(b[12:], uint64(usr))
	// set seq at [20:] inside mutex section inside doPersist

	return dp.addJobToQueue(ctx, persistJob{
		Bytes:  b,
		Evt:    xevt,
		Buffer: buffer,
	})
}

type evtHeader struct {
	Flags uint32
	Kind  uint32
	Seq   int64
	Usr   models.Uid
	Len   uint32
}

func (eh *evtHeader) Len64() int64 {
	return int64(eh.Len)
}

const headerSize = 4 + 4 + 4 + 8 + 8

func readHeader(r io.Reader, scratch []byte) (*evtHeader, error) {
	if len(scratch) < headerSize {
		return nil, fmt.Errorf("must pass scratch buffer of at least %d bytes", headerSize)
	}

	scratch = scratch[:headerSize]
	_, err := io.ReadFull(r, scratch)
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	flags := binary.LittleEndian.Uint32(scratch[:4])
	kind := binary.LittleEndian.Uint32(scratch[4:8])
	l := binary.LittleEndian.Uint32(scratch[8:12])
	usr := binary.LittleEndian.Uint64(scratch[12:20])
	seq := binary.LittleEndian.Uint64(scratch[20:28])

	return &evtHeader{
		Flags: flags,
		Kind:  kind,
		Len:   l,
		Usr:   models.Uid(usr),
		Seq:   int64(seq),
	}, nil
}

func (dp *DiskPersistence) writeHeader(ctx context.Context, flags uint32, kind uint32, l uint32, usr uint64, seq int64) error {
	binary.LittleEndian.PutUint32(dp.scratch, flags)
	binary.LittleEndian.PutUint32(dp.scratch[4:], kind)
	binary.LittleEndian.PutUint32(dp.scratch[8:], l)
	binary.LittleEndian.PutUint64(dp.scratch[12:], usr)
	binary.LittleEndian.PutUint64(dp.scratch[20:], uint64(seq))

	nw, err := dp.logfi.Write(dp.scratch)
	if err != nil {
		return err
	}

	if nw != headerSize {
		return fmt.Errorf("only wrote %d bytes for header", nw)
	}

	return nil
}

func (dp *DiskPersistence) uidForDid(ctx context.Context, did string) (models.Uid, error) {
	if uid, ok := dp.didCache.Get(did); ok {
		return uid, nil
	}

	uid, err := dp.uids.DidToUid(ctx, did)
	if err != nil {
		return 0, err
	}

	dp.didCache.Add(did, uid)

	return uid, nil
}

type takedownSet map[models.Uid]struct{}

func (ts *takedownSet) isTakendown(uid models.Uid) bool {
	if ts == nil {
		return false
	}
	_, found := (*ts)[uid]
	return found
}

func (dp *DiskPersistence) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	var logs []LogFileRef
	needslogs := true
	if since != 0 {
		// find the log file that starts before our since
		result := dp.meta.Debug().Order("seq_start desc").Where("seq_start < ?", since).Limit(1).Find(&logs)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 0 {
			needslogs = false
		}
	}

	var takedownUids *takedownSet
	takedownUids = (*takedownSet)(dp.takenDownCache.Load())

	// playback data from all the log files we found, then check the db to see if more were written during playback.
	// repeat a few times but not unboundedly.
	// don't decrease '10' below 2 because we should always do two passes through this if the above before-chunk query was used.
	for i := 0; i < 10; i++ {
		if needslogs {
			if err := dp.meta.Debug().Order("seq_start asc").Find(&logs, "seq_start >= ?", since).Error; err != nil {
				return err
			}
		}

		lastSeq, err := dp.playbackLogfiles(ctx, since, cb, logs, takedownUids)
		if err != nil {
			return err
		}

		// No lastSeq implies that we read until the end of known events
		if lastSeq == nil {
			break
		}

		since = *lastSeq
		needslogs = true
	}

	return nil
}

func (dp *DiskPersistence) playbackLogfiles(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error, logFiles []LogFileRef, takedownUids *takedownSet) (*int64, error) {
	for i, lf := range logFiles {
		lastSeq, err := dp.readEventsFrom(ctx, since, filepath.Join(dp.primaryDir, lf.Path), cb, takedownUids)
		if err != nil {
			return nil, err
		}
		since = 0
		if i == len(logFiles)-1 &&
			lastSeq != nil &&
			(*lastSeq-lf.SeqStart) == dp.eventsPerFile-1 {
			// There may be more log files to read since the last one was full
			return lastSeq, nil
		}
	}

	return nil, nil
}

func postDoNotEmit(flags uint32) bool {
	if flags&(EvtFlagRebased|EvtFlagTakedown) != 0 {
		return true
	}

	return false
}

func (dp *DiskPersistence) readEventsFrom(ctx context.Context, since int64, fn string, cb func(*events.XRPCStreamEvent) error, takedownUids *takedownSet) (*int64, error) {
	fi, err := os.OpenFile(fn, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	if since != 0 {
		lastSeq, err := scanForLastSeq(fi, since)
		if err != nil {
			return nil, err
		}
		if since > lastSeq {
			dp.log.Error("playback cursor is greater than last seq of file checked",
				"since", since,
				"lastSeq", lastSeq,
				"filename", fn,
			)
			return nil, nil
		}
	}

	bufr := bufio.NewReader(fi)

	lastSeq := int64(0)

	scratch := make([]byte, headerSize)
	for {
		h, err := readHeader(bufr, scratch)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return &lastSeq, nil
			}

			return nil, err
		}

		lastSeq = h.Seq

		if takedownUids.isTakendown(h.Usr) || postDoNotEmit(h.Flags) {
			// event taken down, skip
			_, err := bufr.Discard(int(h.Len))
			if err != nil {
				return nil, fmt.Errorf("failed while skipping event (seq: %d, fn: %q): %w", h.Seq, fn, err)
			}
			continue
		}

		switch h.Kind {
		case evtKindCommit:
			var evt atproto.SyncSubscribeRepos_Commit
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoCommit: &evt}); err != nil {
				return nil, err
			}
		case evtKindSync:
			var evt atproto.SyncSubscribeRepos_Sync
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoSync: &evt}); err != nil {
				return nil, err
			}
		case evtKindHandle:
			var evt atproto.SyncSubscribeRepos_Handle
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoHandle: &evt}); err != nil {
				return nil, err
			}
		case evtKindIdentity:
			var evt atproto.SyncSubscribeRepos_Identity
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoIdentity: &evt}); err != nil {
				return nil, err
			}
		case evtKindAccount:
			var evt atproto.SyncSubscribeRepos_Account
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoAccount: &evt}); err != nil {
				return nil, err
			}
		case evtKindTombstone:
			var evt atproto.SyncSubscribeRepos_Tombstone
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return nil, err
			}
			evt.Seq = h.Seq
			if err := cb(&events.XRPCStreamEvent{RepoTombstone: &evt}); err != nil {
				return nil, err
			}
		default:
			dp.log.Warn("unrecognized event kind coming from log file", "seq", h.Seq, "kind", h.Kind)
			return nil, fmt.Errorf("halting on unrecognized event kind")
		}
	}
}

func (dp *DiskPersistence) dbSetRepoTakedown(ctx context.Context, usr models.Uid) error {
	td := DiskPersistTakedown{
		Uid: usr,
	}
	err := dp.meta.Create(&td).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		// already there, okay!
		return nil
	}
	return err
}
func (dp *DiskPersistence) dbClearRepoTakedown(ctx context.Context, usr models.Uid) error {
	var takedown DiskPersistTakedown
	dp.meta.Model(&takedown).Delete(&takedown)
	result := dp.meta.Model(&takedown).First(&takedown, "uid = ?", usr)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// already gone, no problem
			return nil
		}
	}
	return result.Error
}

func (dp *DiskPersistence) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	takedownsP := dp.takenDownCache.Load()
	if takedownsP != nil {
		_, found := (*takedownsP)[usr]
		if found {
			// already in cache, okay
			return nil
		}
	}
	err := dp.dbSetRepoTakedown(ctx, usr)
	if err != nil {
		return err
	}
	dp.takenDownUpdateLock.Lock()
	defer dp.takenDownUpdateLock.Unlock()
	takedownsP = dp.takenDownCache.Load()
	if takedownsP == nil {
		newTakedowns := make(map[models.Uid]struct{}, 1)
		newTakedowns[usr] = struct{}{}
		dp.takenDownCache.Store(&newTakedowns)
	} else {
		newTakedowns := make(map[models.Uid]struct{}, len(*takedownsP)+1)
		for xu, _ := range *takedownsP {
			newTakedowns[xu] = struct{}{}
		}
		newTakedowns[usr] = struct{}{}
		dp.takenDownCache.Store(&newTakedowns)
	}
	return nil
}
func (dp *DiskPersistence) ReverseTakeDownRepo(ctx context.Context, usr models.Uid) error {
	takedownsP := dp.takenDownCache.Load()
	if takedownsP == nil {
		// nothing is there, ignore
		return nil
	}
	_, found := (*takedownsP)[usr]
	if !found {
		// already gone, okay
		return nil
	}
	err := dp.dbClearRepoTakedown(ctx, usr)
	if err != nil {
		return err
	}
	dp.takenDownUpdateLock.Lock()
	defer dp.takenDownUpdateLock.Unlock()
	takedownsP = dp.takenDownCache.Load()
	_, found = (*takedownsP)[usr]
	if !found {
		// already gone, okay
		return nil
	}
	oldTakedowns := *takedownsP
	newTakedowns := make(map[models.Uid]struct{}, len(oldTakedowns)-1)
	for xu, _ := range oldTakedowns {
		if xu != usr {
			newTakedowns[xu] = struct{}{}
		}
	}
	dp.takenDownCache.Store(&newTakedowns)
	return nil
}

func (dp *DiskPersistence) Flush(ctx context.Context) error {
	dp.lk.Lock()
	defer dp.lk.Unlock()
	if len(dp.evtbuf) > 0 {
		return dp.flushLog(ctx)
	}
	return nil
}

func (dp *DiskPersistence) Shutdown(ctx context.Context) error {
	close(dp.shutdown)
	if err := dp.Flush(ctx); err != nil {
		return err
	}

	dp.logfi.Close()
	return nil
}

func (dp *DiskPersistence) SetEventBroadcaster(f func(*events.XRPCStreamEvent)) {
	dp.broadcast = f
}
