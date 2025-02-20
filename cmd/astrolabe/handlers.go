package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/api/agnostic"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	_ "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
)

func (srv *Server) WebHome(c echo.Context) error {
	info := pongo2.Context{}
	return c.Render(http.StatusOK, "home.html", info)
}

func (srv *Server) WebQuery(c echo.Context) error {

	// parse the q query param, redirect based on that
	q := c.QueryParam("q")
	if q == "" {
		return c.Redirect(http.StatusFound, "/")
	}
	if strings.HasPrefix(q, "https://") {
		q = ParseServiceURL(q)
	}
	if strings.HasPrefix(q, "at://") {
		if strings.HasSuffix(q, "/") {
			q = q[0 : len(q)-1]
		}

		aturi, err := syntax.ParseATURI(q)
		if err != nil {
			return err
		}
		if aturi.RecordKey() != "" {
			return c.Redirect(http.StatusFound, fmt.Sprintf("/at/%s/%s/%s", aturi.Authority(), aturi.Collection(), aturi.RecordKey()))
		}
		if aturi.Collection() != "" {
			return c.Redirect(http.StatusFound, fmt.Sprintf("/at/%s/%s", aturi.Authority(), aturi.Collection()))
		}
		return c.Redirect(http.StatusFound, fmt.Sprintf("/at/%s", aturi.Authority()))
	}
	if strings.HasPrefix(q, "did:") {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/account/%s", q))
	}
	_, err := syntax.ParseHandle(q)
	if nil == err {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/account/%s", q))
	}
	return echo.NewHTTPError(400, "failed to parse query")
}

// e.GET("/account/:atid", srv.WebAccount)
func (srv *Server) WebAccount(c echo.Context) error {
	ctx := c.Request().Context()
	//req := c.Request()
	info := pongo2.Context{}

	atid, err := syntax.ParseAtIdentifier(c.Param("atid"))
	if err != nil {
		return echo.NewHTTPError(404, "failed to parse handle or DID")
	}

	ident, err := srv.dir.Lookup(ctx, *atid)
	if err != nil {
		// TODO: proper error page?
		return err
	}

	bdir := identity.BaseDirectory{}
	doc, err := bdir.ResolveDID(ctx, ident.DID)
	if nil == err {
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		info["didDocJSON"] = string(b)
	}
	info["atid"] = atid
	info["ident"] = ident
	info["uri"] = atid
	return c.Render(http.StatusOK, "account.html", info)
}

// e.GET("/at/:atid", srv.WebRepo)
func (srv *Server) WebRepo(c echo.Context) error {
	ctx := c.Request().Context()
	//req := c.Request()
	info := pongo2.Context{}

	atid, err := syntax.ParseAtIdentifier(c.Param("atid"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse handle or DID")
	}

	ident, err := srv.dir.Lookup(ctx, *atid)
	if err != nil {
		// TODO: proper error page?
		return err
	}
	info["atid"] = atid
	info["ident"] = ident
	info["uri"] = fmt.Sprintf("at://%s", atid)

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	desc, err := comatproto.RepoDescribeRepo(ctx, &xrpcc, ident.DID.String())
	if err != nil {
		return err
	}
	info["collections"] = desc.Collections

	return c.Render(http.StatusOK, "repo.html", info)
}

// e.GET("/at/:atid/:collection", srv.WebCollection)
func (srv *Server) WebRepoCollection(c echo.Context) error {
	ctx := c.Request().Context()
	//req := c.Request()
	info := pongo2.Context{}

	atid, err := syntax.ParseAtIdentifier(c.Param("atid"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse handle or DID")
	}

	collection, err := syntax.ParseNSID(c.Param("collection"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse collection NSID")
	}

	ident, err := srv.dir.Lookup(ctx, *atid)
	if err != nil {
		// TODO: proper error page?
		return err
	}
	info["atid"] = atid
	info["ident"] = ident
	info["collection"] = collection
	info["uri"] = fmt.Sprintf("at://%s/%s", atid, collection)

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	cursor := c.QueryParam("cursor")
	// collection string, cursor string, limit int64, repo string, reverse bool, rkeyEnd string, rkeyStart string
	resp, err := agnostic.RepoListRecords(ctx, &xrpcc, collection.String(), cursor, 100, ident.DID.String(), false, "", "")
	if err != nil {
		return err
	}
	recordURIs := make([]syntax.ATURI, len(resp.Records))
	for i, rec := range resp.Records {
		aturi, err := syntax.ParseATURI(rec.Uri)
		if err != nil {
			return err
		}
		recordURIs[i] = aturi
	}
	if resp.Cursor != nil && *resp.Cursor != "" {
		cursor = *resp.Cursor
	}

	info["records"] = resp.Records
	info["recordURIs"] = recordURIs
	info["cursor"] = cursor
	return c.Render(http.StatusOK, "repo_collection.html", info)
}

// e.GET("/at/:atid/:collection/:rkey", srv.WebRecord)
func (srv *Server) WebRepoRecord(c echo.Context) error {
	ctx := c.Request().Context()
	//req := c.Request()
	info := pongo2.Context{}

	atid, err := syntax.ParseAtIdentifier(c.Param("atid"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse handle or DID")
	}

	collection, err := syntax.ParseNSID(c.Param("collection"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse collection NSID")
	}

	rkey, err := syntax.ParseRecordKey(c.Param("rkey"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse record key")
	}

	ident, err := srv.dir.Lookup(ctx, *atid)
	if err != nil {
		// TODO: proper error page?
		return err
	}
	info["atid"] = atid
	info["ident"] = ident
	info["collection"] = collection
	info["rkey"] = rkey
	info["uri"] = fmt.Sprintf("at://%s/%s/%s", atid, collection, rkey)

	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	resp, err := agnostic.RepoGetRecord(ctx, &xrpcc, "", collection.String(), ident.DID.String(), rkey.String())
	if err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("failed to load record: %s", err))
	}

	if nil == resp.Value {
		return fmt.Errorf("empty record in response")
	}

	record, err := data.UnmarshalJSON(*resp.Value)
	if err != nil {
		return fmt.Errorf("fetched record was invalid data: %w", err)
	}
	info["record"] = record

	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	info["recordJSON"] = string(b)

	return c.Render(http.StatusOK, "repo_record.html", info)
}
