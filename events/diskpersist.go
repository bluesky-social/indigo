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
	primaryDir    string
	archiveDir    string
	eventsPerFile int64

	evts  chan *persistJob
	flush chan chan struct{}

	meta *gorm.DB

	broadcast func(*XRPCStreamEvent)

	logfi *os.File

	curSeq int64

	uidCache *lru.ARCCache
	didCache *lru.ARCCache

	buffers *sync.Pool
	scratch []byte

	outbuf *bytes.Buffer
	evtbuf []*persistJob
}

type persistJob struct {
	Buf  []byte
	Evt  *XRPCStreamEvent
	Done chan error
}

type jobResult struct {
	Err error
	Seq int64
}

var _ (EventPersistence) = (*DiskPersistence)(nil)

type DiskPersistOptions struct {
	UIDCacheSize  int
	DIDCacheSize  int
	EventsPerFile int64
}

func DefaultDiskPersistOptions() *DiskPersistOptions {
	return &DiskPersistOptions{
		EventsPerFile: 10000,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
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
		meta:          db,
		primaryDir:    primaryDir,
		archiveDir:    archiveDir,
		buffers:       bufpool,
		uidCache:      uidCache,
		didCache:      didCache,
		evts:          make(chan *persistJob, 1024),
		eventsPerFile: opts.EventsPerFile,
		scratch:       make([]byte, headerSize),
		outbuf:        new(bytes.Buffer),
		flush:         make(chan chan struct{}),
	}

	if err := dp.resumeLog(); err != nil {
		return nil, err
	}

	go dp.persistWorker()

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

		if end > 0 && eh.Seq >= end {
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

		noff, err := fi.Seek(eh.Len, io.SeekCurrent)
		if err != nil {
			return 0, err
		}

		if noff != offset+headerSize+eh.Len {
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

func (p *DiskPersistence) persistWorker() {
	t := time.NewTimer(time.Hour)
	t.Stop()

	var tick <-chan time.Time
	for {
		select {
		case job, ok := <-p.evts:
			if !ok {
				return
			}

			if err := p.doPersist(job); err != nil {
				job.Done <- err
				continue
			} else {
				job.Done <- nil
			}

			if len(p.evtbuf) > 50 {
				if err := p.flushLog(context.TODO()); err != nil {
					log.Errorf("failed to flush disk log: %s", err)
				}
				t.Stop()
				tick = nil
			}

			if len(p.evtbuf) > 0 && tick == nil {
				t.Reset(time.Millisecond * 50)
				tick = t.C
			}
		case <-tick:
			if err := p.flushLog(context.TODO()); err != nil {
				log.Errorf("failed to flush disk log: %s", err)
			}

			tick = nil
		case freq := <-p.flush:
			// ensure that flushing waits until all pending events get through
			// if theres a huge stream of events coming in this might never happen though...
			if len(p.evts) > 0 {
				go func() {
					time.Sleep(time.Millisecond * 50)
					p.flush <- freq
				}()
			} else {
				if len(p.evtbuf) > 0 {
					if err := p.flushLog(context.TODO()); err != nil {
						log.Errorf("failed to flush disk log: %s", err)
					}
					t.Stop()
					tick = nil
				}

				freq <- struct{}{}
			}
		}
	}
}

func (p *DiskPersistence) flushLog(ctx context.Context) error {
	if len(p.evtbuf) == 0 {
		fmt.Println("attempted a double flush")
		return nil
	}

	_, err := io.Copy(p.logfi, p.outbuf)
	if err != nil {
		return err
	}

	p.outbuf.Truncate(0)

	for _, ej := range p.evtbuf {
		p.broadcast(ej.Evt)
	}

	p.evtbuf = p.evtbuf[:0]

	return nil
}

func (p *DiskPersistence) doPersist(j *persistJob) error {
	b := j.Buf
	e := j.Evt
	seq := p.curSeq
	p.curSeq++

	binary.LittleEndian.PutUint64(b[12:], uint64(seq))

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
	defer p.buffers.Put(buffer)

	buffer.Truncate(0)

	buffer.Write(emptyHeader)

	var evtKind uint32
	switch {
	case e.RepoCommit != nil:
		evtKind = evtKindCommit
		if err := e.RepoCommit.MarshalCBOR(buffer); err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
	case e.RepoHandle != nil:
		evtKind = evtKindHandle
		if err := e.RepoHandle.MarshalCBOR(buffer); err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
	default:
		return nil
		// only those two get peristed right now
	}

	b := buffer.Bytes()

	binary.LittleEndian.PutUint32(b, 0)
	binary.LittleEndian.PutUint32(b[4:], evtKind)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)-headerSize))

	done := make(chan error, 1)
	p.evts <- &persistJob{
		Buf:  b,
		Evt:  e,
		Done: done,
	}

	return <-done // NB: getting rid of this channel 'wait for things to be done' thing makes it go a good deal faster
}

type evtHeader struct {
	Flags uint32
	Kind  uint32
	Seq   int64
	Len   int64
}

const headerSize = 4 + 4 + 4 + 8

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
	seq := binary.LittleEndian.Uint64(scratch[12:20])

	return &evtHeader{
		Flags: flags,
		Kind:  kind,
		Len:   int64(l),
		Seq:   int64(seq),
	}, nil
}

func (p *DiskPersistence) writeHeader(ctx context.Context, flags uint32, kind uint32, l uint32, seq int64) error {
	binary.LittleEndian.PutUint32(p.scratch, flags)
	binary.LittleEndian.PutUint32(p.scratch[4:], kind)
	binary.LittleEndian.PutUint32(p.scratch[8:], l)
	binary.LittleEndian.PutUint64(p.scratch[12:], uint64(seq))

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
	var logs []LogFileRef
	if err := p.meta.Debug().Order("seq_start asc").Find(&logs, "seq_start >= ?", since%p.eventsPerFile).Error; err != nil {
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

		switch h.Kind {
		case evtKindCommit:
			var evt atproto.SyncSubscribeRepos_Commit
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len)); err != nil {
				return err
			}
			evt.Seq = h.Seq
			if err := cb(&XRPCStreamEvent{RepoCommit: &evt}); err != nil {
				return err
			}
		case evtKindHandle:
			var evt atproto.SyncSubscribeRepos_Handle
			if err := evt.UnmarshalCBOR(io.LimitReader(bufr, h.Len)); err != nil {
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

func (p *DiskPersistence) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	panic("no")
}

func (p *DiskPersistence) RebaseRepoEvents(ctx context.Context, usr models.Uid) error {
	panic("no")
}

func (p *DiskPersistence) Flush(ctx context.Context) error {
	req := make(chan struct{})
	p.flush <- req
	<-req
	return nil
}

func (p *DiskPersistence) SetEventBroadcaster(f func(*XRPCStreamEvent)) {
	p.broadcast = f
}
