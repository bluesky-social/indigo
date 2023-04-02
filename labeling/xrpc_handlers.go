package labeling

import (
	"bytes"
	"context"
	"fmt"
	"io"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	util "github.com/bluesky-social/indigo/util"

	"github.com/ipfs/go-cid"
)

func (s *Server) handleComAtprotoIdentityResolveHandle(ctx context.Context, handle string) (*atproto.IdentityResolveHandle_Output, error) {
	// only the one handle, for labelmaker
	if handle == "" {
		return &atproto.IdentityResolveHandle_Output{Did: s.user.SigningKey.Public().DID()}, nil
	} else if handle == s.user.Handle {
		return &atproto.IdentityResolveHandle_Output{Did: s.user.Did}, nil
	} else {
		return nil, fmt.Errorf("handle not found: %s", handle)
	}
}

func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context, repo string) (*atproto.RepoDescribeRepo_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, collection string, limit int, repo string, reverse *bool, rkeyEnd string, rkeyStart string) (*atproto.RepoListRecords_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*atproto.ServerDescribeServer_Output, error) {
	invcode := true
	return &atproto.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerDescribeServer_Links{},
	}, nil
}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, earliest, latest string) (io.Reader, error) {
	// TODO: verify the DID/handle
	var userId util.Uid = 1
	var earlyCid cid.Cid
	if earliest != "" {
		cc, err := cid.Decode(earliest)
		if err != nil {
			return nil, err
		}

		earlyCid = cc
	}

	var lateCid cid.Cid
	if latest != "" {
		cc, err := cid.Decode(latest)
		if err != nil {
			return nil, err
		}

		lateCid = cc
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, userId, earlyCid, lateCid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, c string, collection string, rkey string, user string) (*atproto.RepoGetRecord_Output, error) {
	if user != s.user.Did {
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

	reccid, rec, err := s.repoman.GetRecord(ctx, s.user.UserId, collection, rkey, maybeCid)
	if err != nil {
		return nil, fmt.Errorf("repoman GetRecord: %w", err)
	}

	ccstr := reccid.String()
	return &atproto.RepoGetRecord_Output{
		Cid:   &ccstr,
		Uri:   "at://" + s.user.Did + "/" + collection + "/" + rkey,
		Value: &lexutil.LexiconTypeDecoder{rec},
	}, nil
}

func (s *Server) handleComAtprotoSyncGetHead(ctx context.Context, did string) (*atproto.SyncGetHead_Output, error) {
	panic("not yet implemented")
}
