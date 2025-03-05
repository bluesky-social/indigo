package main

import (
	"encoding/json"
	"errors"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/labstack/echo/v4"
)

// GET /xrpc/com.atproto.identity.resolveHandle
func (srv *Server) ResolveHandle(c echo.Context) error {
	ctx := c.Request().Context()

	hdl, err := syntax.ParseHandle(c.QueryParam("handle"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidHandleSyntax",
			Message: err.Error(),
		})
	}

	did, err := srv.dir.ResolveHandle(ctx, hdl)
	if err != nil && errors.Is(err, identity.ErrHandleNotFound) {
		return c.JSON(404, GenericError{
			Error:   "HandleNotFound",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: err.Error(),
		})
	}
	return c.JSON(200, comatproto.IdentityResolveHandle_Output{
		Did: did.String(),
	})
}

// GET /xrpc/com.atproto.identity.resolveDid
func (srv *Server) ResolveDid(c echo.Context) error {
	ctx := c.Request().Context()

	did, err := syntax.ParseDID(c.QueryParam("did"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidDidSyntax",
			Message: err.Error(),
		})
	}

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(404, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(500, GenericError{
			Error:   "InternalError",
			Message: err.Error(),
		})
	}
	return c.JSON(200, comatproto.IdentityResolveDid_Output{
		DidDoc: rawDoc,
	})
}

// helper for resolveIdentity
func (srv *Server) resolveIdentityFromHandle(c echo.Context, handle syntax.Handle) error {
	ctx := c.Request().Context()

	did, err := srv.dir.ResolveHandle(ctx, handle)
	if err != nil && errors.Is(err, identity.ErrHandleNotFound) {
		return c.JSON(404, GenericError{
			Error:   "HandleNotFound",
			Message: err.Error(),
		})
	} else if err != nil {
		srv.logger.Warn("failed handle resolution", "err", err, "handle", handle)
		return c.JSON(502, GenericError{
			Error:   "HandleResolutionFailed",
			Message: err.Error(),
		})
	}

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(404, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(502, GenericError{
			Error:   "DIDResolutionFailed",
			Message: err.Error(),
		})
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidDidDocument",
			Message: err.Error(),
		})
	}

	ident := identity.ParseIdentity(&doc)
	declHandle, err := ident.DeclaredHandle()
	if err != nil || declHandle != handle {
		return c.JSON(400, GenericError{
			Error:   "HandleMismatch",
			Message: err.Error(),
		})
	}

	return c.JSON(200, comatproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: handle.String(),
		DidDoc: rawDoc,
	})
}

// helper for resolveIdentity
func (srv *Server) resolveIdentityFromDID(c echo.Context, did syntax.DID) error {
	ctx := c.Request().Context()

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(404, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(502, GenericError{
			Error:   "DIDResolutionFailed",
			Message: err.Error(),
		})
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidDidDocument",
			Message: err.Error(),
		})
	}

	ident := identity.ParseIdentity(&doc)
	handle, err := ident.DeclaredHandle()
	if err != nil {
		// no handle declared, or invalid syntax
		handle = syntax.Handle("handle.invalid")
	}

	checkDID, err := srv.dir.ResolveHandle(ctx, handle)
	if err != nil || checkDID != did {
		handle = syntax.Handle("handle.invalid")
	}

	return c.JSON(200, comatproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: handle.String(),
		DidDoc: rawDoc,
	})
}

// GET /xrpc/com.atproto.identity.resolveIdentity
func (srv *Server) ResolveIdentity(c echo.Context) error {
	// we partially re-implement the "Lookup()" logic here, but returning the full DID document, not `identity.Identity`
	atid, err := syntax.ParseAtIdentifier(c.QueryParam("identifier"))
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidIdentifierSyntax",
			Message: err.Error(),
		})
	}

	handle, err := atid.AsHandle()
	if nil == err {
		return srv.resolveIdentityFromHandle(c, handle)
	}
	did, err := atid.AsDID()
	if nil == err {
		return srv.resolveIdentityFromDID(c, did)
	}
	return fmt.Errorf("unreachable code path")
}

// POST /xrpc/com.atproto.identity.refreshIdentity
func (srv *Server) RefreshIdentity(c echo.Context) error {
	ctx := c.Request().Context()

	var body comatproto.IdentityRefreshIdentity_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidRequestBody",
			Message: err.Error(),
		})
	}

	atid, err := syntax.ParseAtIdentifier(body.Identifier)
	if err != nil {
		return c.JSON(400, GenericError{
			Error:   "InvalidIdentifierSyntax",
			Message: err.Error(),
		})
	}

	did, err := atid.AsDID()
	if nil == err {
		if err := srv.dir.PurgeDID(ctx, did); err != nil {
			return err
		}
		return srv.resolveIdentityFromDID(c, did)
	}
	handle, err := atid.AsHandle()
	if nil == err {
		if err := srv.dir.PurgeHandle(ctx, handle); err != nil {
			return err
		}
		return srv.resolveIdentityFromHandle(c, handle)
	}

	return fmt.Errorf("unreachable code path")
}

type GenericStatus struct {
	Daemon  string `json:"daemon"`
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (s *Server) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "bluepages"})
}

func (srv *Server) WebHome(c echo.Context) error {
	return c.String(200, `
eeeee  e     e   e eeee eeeee eeeee eeeee eeee eeeee 
8   8  8     8   8 8    8   8 8   8 8   8 8    8   " 
8eee8e 8e    8e  8 8eee 8eee8 8eee8 8e    8eee 8eeee 
88   8 88    88  8 88   88    88  8 88 "8 88      88 
88eee8 88eee 88ee8 88ee 88    88  8 88ee8 88ee 8ee88 

This is an AT Protocol Identity Service

Most API routes are under /xrpc/

      Code: https://github.com/bluesky-social/indigo/tree/main/cmd/bluepages
  Protocol: https://atproto.com
	`)

}
