package events

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"
	lru "github.com/hashicorp/golang-lru"
	"gorm.io/gorm"
)

type DiskPersistence struct {
	primaryDir      string
	archiveDir      string
	eventsPerFile   int64
	writeBufferSize int

	evts chan *persistJob

	meta *gorm.DB

	broadcast func(*XRPCStreamEvent)

	logfi *os.File

	curSeq int64

	uidCache *lru.ARCCache
	didCache *lru.ARCCache

	buffers *sync.Pool
	scratch []byte

	outbuf *bytes.Buffer
	evtbuf []persistJob

	lk sync.Mutex
}

type persistJob struct {
	Buf    []byte
	Evt    *XRPCStreamEvent
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

var _ (EventPersistence) = (*DiskPersistence)(nil)

type DiskPersistOptions struct {
	UIDCacheSize    int
	DIDCacheSize    int
	EventsPerFile   int64
	WriteBufferSize int
}

func DefaultDiskPersistOptions() *DiskPersistOptions {
	return &DiskPersistOptions{
		EventsPerFile:   10000,
		UIDCacheSize:    100000,
		DIDCacheSize:    100000,
		WriteBufferSize: 50,
	}
}

func NewDiskPersistence(primaryDir, archiveDir string, db *gorm.DB, opts *DiskPersistOptions) (*DiskPersistence, error) {
	if opts == nil {
		opts = DefaultDiskPersistOptions()
	}

	uidCache, err := lru.NewARC(opts.UIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create uid cache: %w", err)
	}

	didCache, err := lru.NewARC(opts.DIDCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create did cache: %w", err)
	}

	db.AutoMigrate(&LogFileRef{})

	bufpool := &sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}

	dp := &DiskPersistence{
		meta:            db,
		primaryDir:      primaryDir,
		archiveDir:      archiveDir,
		buffers:         bufpool,
		uidCache:        uidCache,
		didCache:        didCache,
		evts:            make(chan *persistJob, 1024),
		eventsPerFile:   opts.EventsPerFile,
		scratch:         make([]byte, headerSize),
		outbuf:          new(bytes.Buffer),
		writeBufferSize: opts.WriteBufferSize,
	}

	if err := dp.resumeLog(); err != nil {
		return nil, err
	}

	go dp.flushRoutine()

	return dp, nil
}

type LogFileRef struct {
	gorm.Model
	Path     string
	Archived bool
	SeqStart int64
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

	fi, err := os.Open(filepath.Join(dp.primaryDir, lfr.Path))
	if err != nil {
		return err
	}

	seq, err := scanForLastSeq(fi, -1)
	if err != nil {
		return fmt.Errorf("failed to scan log file for last seqno: %w", err)
	}

	dp.curSeq = seq
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
			if err == io.EOF {
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
	evtKindCommit = 1
	evtKindHandle = 2
)

var emptyHeader = make([]byte, headerSize)

func (p *DiskPersistence) addJobToQueue(job persistJob) error {
	p.lk.Lock()
	defer p.lk.Unlock()

	if err := p.doPersist(job); err != nil {
		return err
	}

	// TODO: for some reason replacing this constant with p.writeBufferSize dramatically reduces perf...
	if len(p.evtbuf) > 400 {
		if err := p.flushLog(context.TODO()); err != nil {
			return fmt.Errorf("failed to flush disk log: %w", err)
		}
	}

	return nil
}

func (p *DiskPersistence) flushRoutine() {
	t := time.NewTicker(time.Millisecond * 100)

	for {
		select {
		case <-t.C:
			p.lk.Lock()
			if err := p.flushLog(context.TODO()); err != nil {
				// TODO: this happening is quite bad. Need a recovery strategy
				log.Errorf("failed to flush disk log: %s", err)
			}
			p.lk.Unlock()
		}
	}
}

func (p *DiskPersistence) flushLog(ctx context.Context) error {
	if len(p.evtbuf) == 0 {
		return nil
	}

	_, err := io.Copy(p.logfi, p.outbuf)
	if err != nil {
		return err
	}

	p.outbuf.Truncate(0)

	for _, ej := range p.evtbuf {
		p.broadcast(ej.Evt)
		ej.Buffer.Truncate(0)
		p.buffers.Put(ej.Buffer)
	}

	p.evtbuf = p.evtbuf[:0]

	return nil
}

func (p *DiskPersistence) doPersist(j persistJob) error {
	b := j.Buf
	e := j.Evt
	seq := p.curSeq
	p.curSeq++

	binary.LittleEndian.PutUint64(b[20:], uint64(seq))

	switch {
	case e.RepoCommit != nil:
		e.RepoCommit.Seq = seq
	case e.RepoHandle != nil:
		e.RepoHandle.Seq = seq
	default:
		// only those two get peristed right now
		// we shouldnt actually ever get here...
		return nil
	}

	// TODO: does this guarantee a full write?
	_, err := p.outbuf.Write(b)
	if err != nil {
		return err
	}

	p.evtbuf = append(p.evtbuf, j)

	if seq%p.eventsPerFile == 0 {
		if err := p.flushLog(context.TODO()); err != nil {
			return err
		}

		// time to roll the log file
		if err := p.swapLog(context.TODO()); err != nil {
			return err
		}
	}

	return nil
}

func (p *DiskPersistence) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	buffer := p.buffers.Get().(*bytes.Buffer)

	buffer.Truncate(0)

	buffer.Write(emptyHeader)

	var did string
	var evtKind uint32
	switch {
	case e.RepoCommit != nil:
		evtKind = evtKindCommit
		did = e.RepoCommit.Repo
		if err := e.RepoCommit.MarshalCBOR(buffer); err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
	case e.RepoHandle != nil:
		evtKind = evtKindHandle
		did = e.RepoHandle.Did
		if err := e.RepoHandle.MarshalCBOR(buffer); err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
	default:
		return nil
		// only those two get peristed right now
	}

	usr, err := p.uidForDid(ctx, did)
	if err != nil {
		return err
	}

	b := buffer.Bytes()

	binary.LittleEndian.PutUint32(b, 0)
	binary.LittleEndian.PutUint32(b[4:], evtKind)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)-headerSize))
	binary.LittleEndian.PutUint64(b[12:], uint64(usr))

	return p.addJobToQueue(persistJob{
		Buf:    b,
		Evt:    e,
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

func (p *DiskPersistence) writeHeader(ctx context.Context, flags uint32, kind uint32, l uint32, usr uint64, seq int64) error {
	binary.LittleEndian.PutUint32(p.scratch, flags)
	binary.LittleEndian.PutUint32(p.scratch[4:], kind)
	binary.LittleEndian.PutUint32(p.scratch[8:], l)
	binary.LittleEndian.PutUint64(p.scratch[12:], usr)
	binary.LittleEndian.PutUint64(p.scratch[20:], uint64(seq))

	nw, err := p.logfi.Write(p.scratch)
	if err != nil {
		return err
	}

	if nw != headerSize {
		return fmt.Errorf("only wrote %d bytes for header", nw)
	}

	return nil
}

func (p *DiskPersistence) uidForDid(ctx context.Context, did string) (models.Uid, error) {
	if uid, ok := p.didCache.Get(did); ok {
		return uid.(models.Uid), nil
	}

	var u models.ActorInfo
	if err := p.meta.First(&u, "did = ?", did).Error; err != nil {
		return 0, err
	}

	p.didCache.Add(did, u.Uid)

	return u.Uid, nil
}

func (p *DiskPersistence) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	base := since - (since * p.eventsPerFile)
	var logs []LogFileRef
	if err := p.meta.Debug().Order("seq_start asc").Find(&logs, "seq_start >= ?", base).Error; err != nil {
		return err
	}

	for _, lf := range logs {
		if err := p.readEventsFrom(ctx, since, filepath.Join(p.primaryDir, lf.Path), cb); err != nil {
			return err
		}
		since = 0
	}

	return nil
}

func postDoNotEmit(flags uint32) bool {
	if flags&(EvtFlagRebased|EvtFlagTakedown) != 0 {
		return true
	}

	return false
}

func (p *DiskPersistence) readEventsFrom(ctx context.Context, since int64, fn string, cb func(*XRPCStreamEvent) error) error {
	fi, err := os.OpenFile(fn, os.O_RDONLY, 0)
	if err != nil {
		return err
	}

	if since != 0 {
		_, err := scanForLastSeq(fi, since)
		if err != nil {
			return err
		}
	}

	bufr := bufio.NewReader(fi)

	scratch := make([]byte, headerSize)
	for {
		h, err := readHeader(bufr, scratch)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if postDoNotEmit(h.Flags) {
			// event taken down, skip
			_, err := io.CopyN(io.Discard, bufr, h.Len64()) // would be really nice if the buffered reader had a 'skip' method that does a seek under the hood
			if err != nil {
				return fmt.Errorf("failed while skipping event (seq: %d, fn: %q): %w", h.Seq, fn, err)
			}
			continue
		}

		switch h.Kind {
		case evtKindCommit:
			var evt atproto.SyncSubscribeRepos_Commit
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return err
			}
			evt.Seq = h.Seq
			if err := cb(&XRPCStreamEvent{RepoCommit: &evt}); err != nil {
				return err
			}
		case evtKindHandle:
			var evt atproto.SyncSubscribeRepos_Handle
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len64())); err != nil {
				return err
			}
			evt.Seq = h.Seq
			if err := cb(&XRPCStreamEvent{RepoHandle: &evt}); err != nil {
				return err
			}
		default:
			log.Warnw("unrecognized event kind coming from log file", "seq", h.Seq, "kind", h.Kind)
			return fmt.Errorf("halting on unrecognized event kind")
		}
	}
}

type UserAction struct {
	gorm.Model

	Usr      models.Uid
	RebaseAt int64
	Takedown bool
}

func (p *DiskPersistence) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	/*
		if err := p.meta.Create(&UserAction{
			Usr:      usr,
			Takedown: true,
		}).Error; err != nil {
			return err
		}
	*/

	return p.forEachShardWithUserEvents(ctx, usr, func(ctx context.Context, fn string) error {
		if err := p.deleteEventsForUser(ctx, usr, fn); err != nil {
			return err
		}

		return nil
	})
}

func (p *DiskPersistence) forEachShardWithUserEvents(ctx context.Context, usr models.Uid, cb func(context.Context, string) error) error {
	var refs []LogFileRef
	if err := p.meta.Order("created_at desc").Find(&refs).Error; err != nil {
		return err
	}

	for _, r := range refs {
		mhas, err := p.refMaybeHasUserEvents(ctx, usr, r)
		if err != nil {
			return err
		}

		if mhas {
			var path string
			if r.Archived {
				path = filepath.Join(p.archiveDir, r.Path)
			} else {
				path = filepath.Join(p.primaryDir, r.Path)
			}

			if err := cb(ctx, path); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *DiskPersistence) refMaybeHasUserEvents(ctx context.Context, usr models.Uid, ref LogFileRef) (bool, error) {
	// TODO: lazily computed bloom filters for users in each logfile
	return true, nil
}

type zeroReader struct{}

func (zr *zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func (p *DiskPersistence) deleteEventsForUser(ctx context.Context, usr models.Uid, fn string) error {
	return p.mutateUserEventsInLog(ctx, usr, fn, EvtFlagTakedown, true)
}

func (p *DiskPersistence) mutateUserEventsInLog(ctx context.Context, usr models.Uid, fn string, flag uint32, zeroEvts bool) error {
	fi, err := os.OpenFile(fn, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer fi.Close()
	defer fi.Sync()

	scratch := make([]byte, headerSize)
	var offset int64
	for {
		h, err := readHeader(fi, scratch)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if h.Usr == usr && h.Flags&flag == 0 {
			nflag := h.Flags | flag

			binary.LittleEndian.PutUint32(scratch, nflag)

			if _, err := fi.WriteAt(scratch[:4], offset); err != nil {
				return fmt.Errorf("failed to write updated flag value: %w", err)
			}

			if zeroEvts {
				// sync that write before blanking the event data
				if err := fi.Sync(); err != nil {
					return err
				}

				if _, err := fi.Seek(offset+headerSize, io.SeekStart); err != nil {
					return fmt.Errorf("failed to seek: %w", err)
				}

				_, err := io.CopyN(fi, &zeroReader{}, h.Len64())
				if err != nil {
					return err
				}
			}
		}

		offset += headerSize + h.Len64()
		_, err = fi.Seek(offset, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek: %w", err)
		}
	}
}

func (p *DiskPersistence) RebaseRepoEvents(ctx context.Context, usr models.Uid) error {
	return p.forEachShardWithUserEvents(ctx, usr, func(ctx context.Context, fn string) error {
		return p.mutateUserEventsInLog(ctx, usr, fn, EvtFlagRebased, false)
	})
}

func (p *DiskPersistence) Flush(ctx context.Context) error {
	p.lk.Lock()
	defer p.lk.Unlock()
	if len(p.evtbuf) > 0 {
		return p.flushLog(ctx)
	}
	return nil
}

func (p *DiskPersistence) SetEventBroadcaster(f func(*XRPCStreamEvent)) {
	p.broadcast = f
}
