package labeling

import (
	"bytes"
	"context"
	"fmt"
	"io"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/ipfs/go-cid"
)

func (s *Server) handleComAtprotoAccountGet(ctx context.Context) error {
	// TODO: implementation
	return nil
}

func (s *Server) handleComAtprotoHandleResolve(ctx context.Context, handle string) (*atproto.HandleResolve_Output, error) {
	// TODO: only the one handle
	if handle == "" {
		return &atproto.HandleResolve_Output{Did: s.user.signingKey.DID()}, nil
	} else if handle == s.user.handle {
		return &atproto.HandleResolve_Output{Did: s.user.did}, nil
	} else {
		return nil, fmt.Errorf("handle not found: %s", handle)
	}
}

func (s *Server) handleComAtprotoRepoDescribe(ctx context.Context, user string) (*atproto.RepoDescribe_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, after string, before string, collection string, limit int, reverse *bool, user string) (*atproto.RepoListRecords_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerGetAccountsConfig(ctx context.Context) (*atproto.ServerGetAccountsConfig_Output, error) {
	invcode := true
	return &atproto.ServerGetAccountsConfig_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerGetAccountsConfig_Links{},
	}, nil
}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, from string) (io.Reader, error) {
	// TODO: verify the DID/handle
	var userId uint = 1
	var fromcid cid.Cid
	if from != "" {
		cc, err := cid.Decode(from)
		if err != nil {
			return nil, err
		}

		fromcid = cc
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, userId, fromcid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, c string, collection string, rkey string, user string) (*atproto.RepoGetRecord_Output, error) {
	if user != s.user.did {
		return nil, fmt.Errorf("unknown user: %s", user)
	}

	var maybeCid cid.Cid
	if c != "" {
		cc, err := cid.Decode(c)
		if err != nil {
			return nil, err
		}
		maybeCid = cc
	}

	reccid, rec, err := s.repoman.GetRecord(ctx, s.user.userId, collection, rkey, maybeCid)
	if err != nil {
		return nil, fmt.Errorf("repoman GetRecord: %w", err)
	}

	ccstr := reccid.String()
	return &atproto.RepoGetRecord_Output{
		Cid:   &ccstr,
		Uri:   "at://" + s.user.did + "/" + collection + "/" + rkey,
		Value: lexutil.LexiconTypeDecoder{rec},
	}, nil
}
