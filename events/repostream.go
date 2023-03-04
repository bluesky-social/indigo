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

type LiteStreamHandleFunc func(op repomgr.EventKind, path string, did string, rcid *cid.Cid, rec any) error

func ConsumeRepoStreamLite(ctx context.Context, con *websocket.Conn, cb LiteStreamHandleFunc) error {
	return HandleRepoStream(ctx, con, &RepoStreamCallbacks{
		Append: func(evt *RepoAppend) error {
			r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
			if err != nil {
				return err
			}

			for _, op := range evt.Ops {
				ek := repomgr.EventKind(op.Kind)
				switch ek {
				case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
					rc, rec, err := r.GetRecord(ctx, op.Path)
					if err != nil {
						e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Rec, evt.Seq, evt.Repo, err)
						log.Error(e)
						return nil
					}

					if rc != *op.Rec {
						return fmt.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Rec)
					}

					if err := cb(ek, op.Path, evt.Repo, op.Rec, rec); err != nil {
						return err
					}

				case repomgr.EvtKindDeleteRecord:
					if err := cb(ek, op.Path, evt.Repo, nil, nil); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})
}
