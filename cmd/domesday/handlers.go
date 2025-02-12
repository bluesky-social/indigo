package main

import (
	"fmt"
	"log/slog"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/labstack/echo/v4"
)

type GenericError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (srv *Server) ResolveHandle(c echo.Context) error {
	ctx := c.Request().Context()

	hdl, err := syntax.ParseHandle(c.QueryParam("handle"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidHandleSyntax",
			Message: fmt.Sprintf("%s", err), // TODO: something more idiomatic?
		})
	}

	// XXX: ResolveHandle() on identity
	ident, err := srv.dir.LookupHandle(ctx, hdl)
	if err != nil && false { // XXX: is ErrNotFound; other errors?
		return c.JSON(404, GenericError{
			Error:   "HandleNotFound",
			Message: fmt.Sprintf("%s", err),
		})
	} else if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: fmt.Sprintf("%s", err),
		})
	}
	return c.JSON(200, comatproto.IdentityResolveHandle_Output{
		Did: ident.DID.String(),
	})
}

func (srv *Server) ResolveDid(c echo.Context) error {
	ctx := c.Request().Context()

	did, err := syntax.ParseDID(c.QueryParam("did"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidDidSyntax",
			Message: fmt.Sprintf("%s", err), // TODO: something more idiomatic?
		})
	}

	// XXX: ResolveDID() on identity?
	ident, err := srv.dir.LookupDID(ctx, did)
	if err != nil && false { // XXX: is ErrNotFound; other errors?
		return c.JSON(404, GenericError{
			Error:   "DidNotFound",
			Message: fmt.Sprintf("%s", err),
		})
	} else if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: fmt.Sprintf("%s", err),
		})
	}
	return c.JSON(200, comatproto.IdentityResolveDid_Output{
		DidDoc: ident.DIDDocument(), // XXX
	})
}

func (srv *Server) ResolveIdentity(c echo.Context) error {
	ctx := c.Request().Context()

	atid, err := syntax.ParseAtIdentifier(c.QueryParam("identifier"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidIdentifierSyntax",
			Message: fmt.Sprintf("%s", err), // TODO: something more idiomatic?
		})
	}

	// XXX: ResolveDID() on identity?
	ident, err := srv.dir.Lookup(ctx, *atid)
	if err != nil && false { // XXX: is ErrNotFound; other errors?
		return c.JSON(404, GenericError{
			Error:   "DidNotFound",
			Message: fmt.Sprintf("%s", err),
		})
	} else if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: fmt.Sprintf("%s", err),
		})
	}
	handle := ident.Handle.String()
	return c.JSON(200, comatproto.IdentityDefs_AtprotoIdentity{
		Did:    ident.DID.String(),
		Handle: &handle,
		DidDoc: ident.DIDDocument(), // XXX
	})
}

func (srv *Server) RefreshIdentity(c echo.Context) error {
	ctx := c.Request().Context()

	atid, err := syntax.ParseAtIdentifier(c.QueryParam("identifier"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidIdentifierSyntax",
			Message: fmt.Sprintf("%s", err), // TODO: something more idiomatic?
		})
	}

	err = srv.dir.Purge(ctx, *atid)
	if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: fmt.Sprintf("%s", err),
		})
	}

	return srv.ResolveIdentity(c)
}

type GenericStatus struct {
	Daemon  string `json:"daemon"`
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (srv *Server) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	var errorMessage string
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		errorMessage = fmt.Sprintf("%s", he.Message)
	}
	if code >= 500 {
		slog.Warn("domesday-http-internal-error", "err", err)
	}
	// XXX: actual error struct
	c.JSON(code, GenericStatus{Status: "error", Daemon: "domesday", Message: errorMessage})
}

func (s *Server) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "domesday"})
}

func (srv *Server) WebHome(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "domesday"})
}
