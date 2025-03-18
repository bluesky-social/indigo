package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/labstack/echo/v4"
)

// GET /xrpc/com.atproto.identity.resolveHandle
func (srv *Server) ResolveHandle(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), srv.requestTimeout)
	defer cancel()

	raw := c.QueryParam("handle")
	if raw == "" {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "BadRequest",
			Message: "query parameter missing or empty: handle",
		})
	}
	hdl, err := syntax.ParseHandle(raw)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "InvalidHandleSyntax",
			Message: err.Error(),
		})
	}

	did, err := srv.dir.ResolveHandle(ctx, hdl)
	if err != nil && errors.Is(err, identity.ErrHandleNotFound) {
		return c.JSON(http.StatusNotFound, GenericError{
			Error:   "HandleNotFound",
			Message: err.Error(),
		})
	} else if err != nil && errors.Is(err, identity.ErrHandleResolutionFailed) {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "HandleResolutionFailed",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, GenericError{
			Error:   "InternalServerError",
			Message: err.Error(),
		})
	}
	return c.JSON(http.StatusOK, comatproto.IdentityResolveHandle_Output{
		Did: did.String(),
	})
}

// GET /xrpc/com.atproto.identity.resolveDid
func (srv *Server) ResolveDid(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), srv.requestTimeout)
	defer cancel()

	raw := c.QueryParam("did")
	if raw == "" {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "BadRequest",
			Message: "query parameter missing or empty: did",
		})
	}
	did, err := syntax.ParseDID(raw)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "InvalidDidSyntax",
			Message: err.Error(),
		})
	}

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(http.StatusNotFound, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil && errors.Is(err, identity.ErrDIDResolutionFailed) {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "DidResolutionFailed",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, GenericError{
			Error:   "InternalServerError",
			Message: err.Error(),
		})
	}
	return c.JSON(http.StatusOK, comatproto.IdentityResolveDid_Output{
		DidDoc: rawDoc,
	})
}

// helper for resolveIdentity
func (srv *Server) resolveIdentityFromHandle(c echo.Context, handle syntax.Handle) error {
	ctx, cancel := context.WithTimeout(context.Background(), srv.requestTimeout)
	defer cancel()

	did, err := srv.dir.ResolveHandle(ctx, handle)
	if err != nil && errors.Is(err, identity.ErrHandleNotFound) {
		return c.JSON(http.StatusNotFound, GenericError{
			Error:   "HandleNotFound",
			Message: err.Error(),
		})
	} else if err != nil && errors.Is(err, identity.ErrHandleResolutionFailed) {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "HandleResolutionFailed",
			Message: err.Error(),
		})
	} else if err != nil {
		srv.logger.Warn("handle resolution error", "err", err, "handle", handle)
		return c.JSON(http.StatusInternalServerError, GenericError{
			Error:   "InternalServerError",
			Message: err.Error(),
		})
	}

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(http.StatusNotFound, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil && errors.Is(err, identity.ErrDIDResolutionFailed) {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "DidResolutionFailed",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, GenericError{
			Error:   "InternalServerError",
			Message: err.Error(),
		})
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "InvalidDidDocument",
			Message: err.Error(),
		})
	}

	ident := identity.ParseIdentity(&doc)
	declHandle, err := ident.DeclaredHandle()
	if err != nil || declHandle != handle {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "HandleMismatch",
			Message: err.Error(),
		})
	}

	return c.JSON(http.StatusOK, comatproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: handle.String(),
		DidDoc: rawDoc,
	})
}

// helper for resolveIdentity
func (srv *Server) resolveIdentityFromDID(c echo.Context, did syntax.DID) error {
	ctx, cancel := context.WithTimeout(context.Background(), srv.requestTimeout)
	defer cancel()

	rawDoc, err := srv.dir.ResolveDIDRaw(ctx, did)
	if err != nil && errors.Is(err, identity.ErrDIDNotFound) {
		return c.JSON(http.StatusNotFound, GenericError{
			Error:   "DidNotFound",
			Message: err.Error(),
		})
	} else if err != nil && errors.Is(err, identity.ErrDIDResolutionFailed) {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "DidResolutionFailed",
			Message: err.Error(),
		})
	} else if err != nil {
		return c.JSON(http.StatusBadGateway, GenericError{
			Error:   "DIDResolutionFailed",
			Message: err.Error(),
		})
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
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

	return c.JSON(http.StatusOK, comatproto.IdentityDefs_IdentityInfo{
		Did:    ident.DID.String(),
		Handle: handle.String(),
		DidDoc: rawDoc,
	})
}

// GET /xrpc/com.atproto.identity.resolveIdentity
func (srv *Server) ResolveIdentity(c echo.Context) error {
	// we partially re-implement the "Lookup()" logic here, but returning the full DID document, not `identity.Identity`
	raw := c.QueryParam("identifier")
	if raw == "" {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "BadRequest",
			Message: "query parameter missing or empty: identifier",
		})
	}
	atid, err := syntax.ParseAtIdentifier(raw)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
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
	ctx, cancel := context.WithTimeout(context.Background(), srv.requestTimeout)
	defer cancel()

	var body comatproto.IdentityRefreshIdentity_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "BadRequest",
			Message: err.Error(),
		})
	}

	atid, err := syntax.ParseAtIdentifier(body.Identifier)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GenericError{
			Error:   "InvalidIdentifierSyntax",
			Message: err.Error(),
		})
	}

	did, err := atid.AsDID()
	if nil == err {
		if err := srv.dir.PurgeDID(ctx, did); err != nil {
			return c.JSON(http.StatusInternalServerError, GenericError{
				Error:   "InternalServerError",
				Message: err.Error(),
			})
		}
		return srv.resolveIdentityFromDID(c, did)
	}
	handle, err := atid.AsHandle()
	if nil == err {
		if err := srv.dir.PurgeHandle(ctx, handle); err != nil {
			return c.JSON(http.StatusInternalServerError, GenericError{
				Error:   "InternalServerError",
				Message: err.Error(),
			})
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
	return c.JSON(http.StatusOK, GenericStatus{Status: "ok", Daemon: "bluepages"})
}

func (srv *Server) WebHome(c echo.Context) error {
	return c.String(http.StatusOK, `
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
