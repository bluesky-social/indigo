package events

import (
	"context"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"

	"github.com/gorilla/websocket"
)

type RepoStreamCallbacks struct {
	RepoCommit    func(evt *comatproto.SyncSubscribeRepos_Commit) error
	RepoHandle    func(evt *comatproto.SyncSubscribeRepos_Handle) error
	RepoInfo      func(evt *comatproto.SyncSubscribeRepos_Info) error
	RepoMigrate   func(evt *comatproto.SyncSubscribeRepos_Migrate) error
	RepoTombstone func(evt *comatproto.SyncSubscribeRepos_Tombstone) error
	LabelBatch    func(evt *LabelBatch) error
	Error         func(evt *ErrorFrame) error
}

func HandleRepoStream(ctx context.Context, con *websocket.Conn, cbs *RepoStreamCallbacks) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
				return
			}
		}
	}()

	lastSeq := int64(-1)
	for {
		mt, r, err := con.NextReader()
		if err != nil {
			return err
		}

		switch mt {
		default:
			return fmt.Errorf("expected binary message from subscription endpoint")
		case websocket.BinaryMessage:
			// ok
		}

		var header EventHeader
		if err := header.UnmarshalCBOR(r); err != nil {
			return fmt.Errorf("reading header: %w", err)
		}

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

				if cbs.RepoCommit != nil {
					if err := cbs.RepoCommit(&evt); err != nil {
						return err
					}
				} else {
					log.Warnf("received repo commit event with nil commit object (seq %d)", evt.Seq)
				}
			case "#info":
				var evt comatproto.SyncSubscribeRepos_Info
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if cbs.RepoInfo != nil {
					if err := cbs.RepoInfo(&evt); err != nil {
						return err
					}
				}
			case "#migrate":
				var evt comatproto.SyncSubscribeRepos_Migrate
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if cbs.RepoMigrate != nil {
					if err := cbs.RepoMigrate(&evt); err != nil {
						return err
					}
				}
			case "#tombstone":
				var evt comatproto.SyncSubscribeRepos_Tombstone
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if cbs.RepoMigrate != nil {
					if err := cbs.RepoTombstone(&evt); err != nil {
						return err
					}
				}
			case "#labebatch":
				var evt LabelBatch
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading LabelBatch event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
				}

				lastSeq = evt.Seq

				if cbs.LabelBatch != nil {
					if err := cbs.LabelBatch(&evt); err != nil {
						return err
					}
				} else {
					log.Warnf("received label event with nil append object (seq %d)", evt.Seq)
				}
			}

		case EvtKindErrorFrame:
			var errframe ErrorFrame
			if err := errframe.UnmarshalCBOR(r); err != nil {
				return err
			}

			if cbs.Error != nil {
				if err := cbs.Error(&errframe); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unrecognized event stream type: %d", header.Op)
		}

	}
}
