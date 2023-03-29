package events

import (
	"bytes"
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/gorilla/websocket"
	cid "github.com/ipfs/go-cid"
)

type LiteStreamHandleFunc func(op repomgr.EventKind, seq int64, path string, did string, rcid *cid.Cid, rec any) error

func ConsumeRepoStreamLite(ctx context.Context, con *websocket.Conn, cb LiteStreamHandleFunc) error {
	return HandleRepoStream(ctx, con, &RepoStreamCallbacks{
		RepoAppend: func(evt *RepoAppend) error {
			if evt.TooBig {
				log.Errorf("skipping too big events for now: %d", evt.Seq)
				return nil
			}
			r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
			if err != nil {
				return fmt.Errorf("reading repo from car (seq: %d, len: %d): %w", evt.Seq, len(evt.Blocks), err)
			}

			for _, op := range evt.Ops {
				ek := repomgr.EventKind(op.Action)
				switch ek {
				case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
					rc, rec, err := r.GetRecord(ctx, op.Path)
					if err != nil {
						e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Cid, evt.Seq, evt.Repo, err)
						log.Error(e)
						return nil
					}

					if rc != *op.Cid {
						return fmt.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
					}

					if err := cb(ek, evt.Seq, op.Path, evt.Repo, &rc, rec); err != nil {
						return err
					}

				case repomgr.EvtKindDeleteRecord:
					if err := cb(ek, evt.Seq, op.Path, evt.Repo, nil, nil); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})
}
