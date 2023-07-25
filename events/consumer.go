package events

import (
	"context"
	"fmt"
	"io"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/gorilla/websocket"
)

type RepoStreamCallbacks struct {
	RepoCommit    func(evt *comatproto.SyncSubscribeRepos_Commit) error
	RepoHandle    func(evt *comatproto.SyncSubscribeRepos_Handle) error
	RepoInfo      func(evt *comatproto.SyncSubscribeRepos_Info) error
	RepoMigrate   func(evt *comatproto.SyncSubscribeRepos_Migrate) error
	RepoTombstone func(evt *comatproto.SyncSubscribeRepos_Tombstone) error
	LabelLabels   func(evt *label.SubscribeLabels_Labels) error
	LabelInfo     func(evt *label.SubscribeLabels_Info) error
	Error         func(evt *ErrorFrame) error
}

func (rsc *RepoStreamCallbacks) EventHandler(ctx context.Context, xev *XRPCStreamEvent) error {
	switch {
	case xev.RepoCommit != nil && rsc.RepoCommit != nil:
		return rsc.RepoCommit(xev.RepoCommit)
	case xev.RepoHandle != nil && rsc.RepoHandle != nil:
		return rsc.RepoHandle(xev.RepoHandle)
	case xev.RepoInfo != nil && rsc.RepoInfo != nil:
		return rsc.RepoInfo(xev.RepoInfo)
	case xev.RepoMigrate != nil && rsc.RepoMigrate != nil:
		return rsc.RepoMigrate(xev.RepoMigrate)
	case xev.RepoTombstone != nil && rsc.RepoTombstone != nil:
		return rsc.RepoTombstone(xev.RepoTombstone)
	case xev.LabelLabels != nil && rsc.LabelLabels != nil:
		return rsc.LabelLabels(xev.LabelLabels)
	case xev.LabelInfo != nil && rsc.LabelInfo != nil:
		return rsc.LabelInfo(xev.LabelInfo)
	case xev.Error != nil && rsc.Error != nil:
		return rsc.Error(xev.Error)
	default:
		return nil
	}
}

type instrumentedReader struct {
	r            io.Reader
	addr         string
	bytesCounter prometheus.Counter
}

func (sr *instrumentedReader) Read(p []byte) (int, error) {
	n, err := sr.r.Read(p)
	sr.bytesCounter.Add(float64(n))
	return n, err
}

func HandleRepoStream(ctx context.Context, con *websocket.Conn, sched Scheduler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	remoteAddr := con.RemoteAddr().String()

	go func() {
		t := time.NewTicker(time.Second * 30)
		defer t.Stop()

		for {
			select {
			case <-t.C:
				if err := con.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second*10)); err != nil {
					log.Warnf("failed to ping: %s", err)
				}
			case <-ctx.Done():
				con.Close()
				return
			}
		}
	}()

	lastSeq := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		mt, rawReader, err := con.NextReader()
		if err != nil {
			return err
		}

		switch mt {
		default:
			return fmt.Errorf("expected binary message from subscription endpoint")
		case websocket.BinaryMessage:
			// ok
		}

		r := &instrumentedReader{
			r:            rawReader,
			addr:         remoteAddr,
			bytesCounter: bytesFromStreamCounter.WithLabelValues(remoteAddr),
		}

		var header EventHeader
		if err := header.UnmarshalCBOR(r); err != nil {
			return fmt.Errorf("reading header: %w", err)
		}

		eventsFromStreamCounter.WithLabelValues(remoteAddr).Inc()

		switch header.Op {
		case EvtKindMessage:
			switch header.MsgType {
			case "#commit":
				var evt comatproto.SyncSubscribeRepos_Commit
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading repoCommit event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Repo, &XRPCStreamEvent{
					RepoCommit: &evt,
				}); err != nil {
					return err
				}
			case "#handle":
				var evt comatproto.SyncSubscribeRepos_Handle
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoHandle: &evt,
				}); err != nil {
					return err
				}
			case "#info":
				// TODO: this might also be a LabelInfo (as opposed to RepoInfo)
				var evt comatproto.SyncSubscribeRepos_Info
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
					RepoInfo: &evt,
				}); err != nil {
					return err
				}
			case "#migrate":
				var evt comatproto.SyncSubscribeRepos_Migrate
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoMigrate: &evt,
				}); err != nil {
					return err
				}
			case "#tombstone":
				var evt comatproto.SyncSubscribeRepos_Tombstone
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoTombstone: &evt,
				}); err != nil {
					return err
				}
			case "#labebatch":
				var evt label.SubscribeLabels_Labels
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading Labels event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
					LabelLabels: &evt,
				}); err != nil {
					return err
				}
			}

		case EvtKindErrorFrame:
			var errframe ErrorFrame
			if err := errframe.UnmarshalCBOR(r); err != nil {
				return err
			}

			if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
				Error: &errframe,
			}); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unrecognized event stream type: %d", header.Op)
		}

	}
}
