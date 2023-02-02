package labeling

import (
	"bytes"
	"context"
	"fmt"
	"io"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	// TODO: which PDS methods need to implemented? some examples below
	//e.GET("/xrpc/com.atproto.account.get", s.HandleComAtprotoAccountGet)
	//e.GET("/xrpc/com.atproto.handle.resolve", s.HandleComAtprotoHandleResolve)
	//e.GET("/xrpc/com.atproto.repo.describe", s.HandleComAtprotoRepoDescribe)
	e.GET("/xrpc/com.atproto.repo.getRecord", s.HandleComAtprotoRepoGetRecord)
	//e.GET("/xrpc/com.atproto.repo.listRecords", s.HandleComAtprotoRepoListRecords)
	//e.GET("/xrpc/com.atproto.server.getAccountsConfig", s.HandleComAtprotoServerGetAccountsConfig)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	//e.GET("/xrpc/com.atproto.sync.getRoot", s.HandleComAtprotoSyncGetRoot)
	return nil
}

func (s *Server) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	from := c.QueryParam("from")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context,did string,from string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, from)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/octet-stream", out)
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

func (s *Server) HandleComAtprotoRepoGetRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoGetRecord")
	defer span.End()
	cid := c.QueryParam("cid")
	collection := c.QueryParam("collection")
	rkey := c.QueryParam("rkey")
	user := c.QueryParam("user")
	var out *atproto.RepoGetRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context,cid string,collection string,rkey string,user string) (*comatprototypes.RepoGetRecord_Output, error)
	out, handleErr = s.handleComAtprotoRepoGetRecord(ctx, cid, collection, rkey, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
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
