/*
.car file remote interface
*/
package remote

import (
	"context"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
)

type CarfileRemote struct {
	Filepath string
}

func (self CarfileRemote) ListRepos(params ListReposParams) (*ListReposResult, error) {
	ctx := context.Background()

	_, _, did, err := ReadCar(ctx, self.Filepath)
	if err != nil {
		return nil, err
	}

	res := ListReposResult{
		Dids: []string{did},
	}
	return &res, nil
}

func (self CarfileRemote) FetchRepo(params FetchRepoParams) (*RemoteStream, error) {
	ctx := context.Background()

	// read & validate
	_, r, did, err := ReadCar(ctx, self.Filepath)
	if err != nil {
		return nil, err
	}
	if did != params.Did {
		return nil, fmt.Errorf("Repo not found: %s", params.Did)
	}

	res := RemoteStream{Ch: make(chan StreamEvent)}

	// walk & emit
	go (func() {
		err = r.MST.Walk(func(k []byte, v cid.Cid) error {
			col, rkey, err := syntax.ParseRepoPath(string(k))
			if err != nil {
				return err
			}
			recBytes, _, err := r.GetRecordBytes(ctx, col, rkey)
			if err != nil {
				return err
			}

			rec, err := data.UnmarshalCBOR(recBytes)
			if err != nil {
				return err
			}

			res.Ch <- StreamEvent{
				Did:  did,
				Time: 0, // TODO
				Kind: "commit",
				Commit: StreamEventCommit{
					Rev:        "", // TODO
					Operation:  "create",
					Collection: col.String(),
					Rkey:       rkey.String(),
					Record:     rec,
					Cid:        "", // TODO
				},
			}
			return nil
		})

		if err != nil {
			res.Ch <- StreamEvent{
				Did:   did,
				Time:  0, // TODO
				Kind:  "Error",
				Error: StreamEventError{Err: err},
			}
		}

		close(res.Ch)
	})()

	return &res, nil
}

func (self CarfileRemote) SubscribeRecords(params SubscribeRecordsParams) (*RemoteStream, error) {
	res := RemoteStream{Ch: make(chan StreamEvent)}
	close(res.Ch)
	return &res, nil
}

func ReadCar(ctx context.Context, path string) (*repo.Commit, *repo.Repo, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, "", err
	}
	c, r, err := repo.LoadRepoFromCAR(ctx, file)
	if err != nil {
		return nil, nil, "", err
	}
	did, err := syntax.ParseDID(c.DID)
	if err != nil {
		return nil, nil, "", err
	}
	return c, r, did.String(), nil
}
