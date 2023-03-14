package events

import (
	"context"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type RepoStreamCallbacks struct {
	RepoAppend func(evt *RepoAppend) error
	LabelBatch func(evt *LabelBatch) error
	Info       func(evt *InfoFrame) error
	Error      func(evt *ErrorFrame) error
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
		case EvtKindRepoAppend:
			var evt RepoAppend
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading repoAppend event: %w", err)
			}

			if evt.Seq < lastSeq {
				log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
			}

			lastSeq = evt.Seq

			if cbs.RepoAppend != nil {
				if err := cbs.RepoAppend(&evt); err != nil {
					return err
				}
			} else {
				log.Warnf("received repo append event with nil append object (seq %d)", evt.Seq)
			}
		case EvtKindLabelBatch:
			var evt LabelBatch
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading LabelBatch event: %w", err)
			}

			if evt.Seq < lastSeq {
				log.Errorf("Got events out of order from stream (seq = %d, prev = %d)", evt.Seq, lastSeq)
			}

			lastSeq = evt.Seq

			if cbs.RepoAppend != nil {
				if err := cbs.LabelBatch(&evt); err != nil {
					return err
				}
			} else {
				log.Warnf("received label event with nil append object (seq %d)", evt.Seq)
			}
		case EvtKindInfoFrame:
			var info InfoFrame
			if err := info.UnmarshalCBOR(r); err != nil {
				return err
			}

			if cbs.Info != nil {
				if err := cbs.Info(&info); err != nil {
					return err
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
