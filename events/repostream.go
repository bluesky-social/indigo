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

type GetRepoFunc func(ctx context.Context, did string, oldest *string, newest string) ([]byte, error)

func ConsumeRepoStreamLite(ctx context.Context, con *websocket.Conn, cb LiteStreamHandleFunc, getrepo GetRepoFunc) error {
	return HandleRepoStream(ctx, con, &RepoStreamCallbacks{
		RepoAppend: func(evt *RepoAppend) error {
			if evt.TooBig {
				if evt.Prev != nil {
					log.Errorf("skipping non-genesis too big events for now: %d", evt.Seq)
					return nil
				}

				slice, err := getrepo(ctx, evt.Repo, nil, evt.Commit)
				if err != nil {
					return fmt.Errorf("fetching repo data for 'too big' event slice failed (seq: %d, remote: %s): %w", evt.Seq, con.RemoteAddr(), err)
				}

				rep, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(slice))
				if err != nil {
					return err
				}

				return rep.ForEach(ctx, "", func(k string, v cid.Cid) error {
					rc, rec, err := rep.GetRecord(ctx, k)
					if err != nil {
						return fmt.Errorf("failed to load referenced record from repo (rec: %s, seq: %d): %w", k, evt.Seq, err)
					}

					if err := cb(repomgr.EvtKindCreateRecord, evt.Seq, k, evt.Repo, &rc, rec); err != nil {
						return err
					}
					return nil
				})
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

					if rc.String() != *op.Cid {
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
