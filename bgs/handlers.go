package bgs

import (
	"bytes"
	"context"
	"io"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/ipfs/go-cid"
)

func (s *BGS) handleComAtprotoSyncGetCheckout(ctx context.Context, commit string, did string) (io.Reader, error) {
	/*
		u, err := s.Index.LookupUserByDid(ctx, did)
		if err != nil {
			return nil, err
		}

		c, err := cid.Decode(commit)
		if err != nil {
			return nil, err
		}

		// TODO: need to enable a 'write to' interface for codegenned things...
		buf := new(bytes.Buffer)
		if err := s.repoman.GetCheckout(ctx, u.Uid, c, buf); err != nil {
			return nil, err
		}

		return buf, nil
	*/
	panic("nyi")
}

func (s *BGS) handleComAtprotoSyncGetCommitPath(ctx context.Context, did string, earliest string, latest string) (*comatprototypes.SyncGetCommitPath_Output, error) {
	panic("nyi")
}

func (s *BGS) handleComAtprotoSyncGetHead(ctx context.Context, did string) (*comatprototypes.SyncGetHead_Output, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	root, err := s.repoman.GetRepoRoot(ctx, u.Uid)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SyncGetHead_Output{
		Root: root.String(),
	}, nil
}

func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context, collection string, commit string, did string, rkey string) (io.Reader, error) {
	panic("nyi")
}

func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context, did string, earliest string, latest string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	var earlyCid, lateCid cid.Cid
	if earliest != "" {
		c, err := cid.Decode(earliest)
		if err != nil {
			return nil, err
		}

		earlyCid = c
	}

	if latest != "" {
		c, err := cid.Decode(latest)
		if err != nil {
			return nil, err
		}

		lateCid = c
	}

	// TODO: stream the response
	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, u.Uid, earlyCid, lateCid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetBlocks(ctx context.Context, cids []string, did string) (io.Reader, error) {
	panic("NYI")
}

func (s *BGS) handleComAtprotoSyncRequestCrawl(ctx context.Context, host string) error {
	log.Warnf("TODO: host validation for crawl requests")
	return s.slurper.SubscribeToPds(ctx, host, true)
}

func (s *BGS) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context) error {
	panic("NYI")
	//return s.slurper.SubscribeToPds(ctx, host, false)
}
