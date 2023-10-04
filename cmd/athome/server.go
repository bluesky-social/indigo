package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"embed"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/urfave/cli/v2"
)

//go:embed static/*
var StaticFS embed.FS

type Server struct {
	xrpcc *xrpc.Client
}

func serve(cctx *cli.Context) error {
	debug := cctx.Bool("debug")
	httpAddress := cctx.String("http-address")
	pdsHost := cctx.String("pds-host")
	atpHandle := cctx.String("handle")
	atpPassword := cctx.String("password")

	// create a new session
	// TODO: does this work with no auth at all?
	xrpcc := &xrpc.Client{
		Client: cliutil.NewHttpClient(),
		Host:   pdsHost,
		Auth: &xrpc.AuthInfo{
			Handle: atpHandle,
		},
	}

	auth, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
		Identifier: xrpcc.Auth.Handle,
		Password:   atpPassword,
	})
	if err != nil {
		return err
	}
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt
	xrpcc.Auth.Did = auth.Did
	xrpcc.Auth.Handle = auth.Handle

	server := Server{xrpcc}

	staticHandler := http.FileServer(func() http.FileSystem {
		if debug {
			return http.FS(os.DirFS("static"))
		}
		fsys, err := fs.Sub(StaticFS, "static")
		if err != nil {
			log.Fatal(err)
		}
		return http.FS(fsys)
	}())

	e := echo.New()
	e.HideBanner = true
	// SECURITY: Do not modify without due consideration.
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "SAMEORIGIN",
		HSTSMaxAge:         31536000, // 365 days
		// TODO:
		// ContentSecurityPolicy
		// XSSProtection
	}))
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		// Don't log requests for static content.
		Skipper: func(c echo.Context) bool {
			return strings.HasPrefix(c.Request().URL.Path, "/static")
		},
		Format: "method=${method} path=${uri} status=${status} latency=${latency_human}\n",
	}))
	e.Renderer = NewRenderer("templates/", &TemplateFS, debug)
	e.HTTPErrorHandler = customHTTPErrorHandler

	// redirect trailing slash to non-trailing slash.
	// all of our current endpoints have no trailing slash.
	e.Use(middleware.RemoveTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		RedirectCode: http.StatusFound,
	}))

	// basic static routes
	e.GET("/robots.txt", echo.WrapHandler(staticHandler))
	e.GET("/favicon.ico", echo.WrapHandler(staticHandler))
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", staticHandler)))
	e.GET("/", server.WebHome)

	// generic routes
	//e.GET("/search", server.WebGeneric)
	//e.GET("/support", server.WebGeneric)
	//e.GET("/support/privacy", server.WebGeneric)
	//e.GET("/support/tos", server.WebGeneric)
	//e.GET("/support/community-guidelines", server.WebGeneric)
	//e.GET("/support/copyright", server.WebGeneric)

	// profile endpoints; only first populates info
	e.GET("/profile/:handle", server.WebProfile)
	//e.GET("/profile/:handle/repo.car.gz", server.WebProfile)
	//e.GET("/profile/:handle/follows", server.WebGeneric)
	//e.GET("/profile/:handle/followers", server.WebGeneric)

	// post endpoints; only first populates info
	e.GET("/profile/:handle/post/:rkey", server.WebPost)
	//e.GET("/profile/:handle/post/:rkey/liked-by", server.WebGeneric)
	//e.GET("/profile/:handle/post/:rkey/reposted-by", server.WebGeneric)

	// feeds
	e.GET("/feed/:name", server.WebFeed)

	// redirect
	//e.GET("/at://:account", server.WebAccountURI)
	//e.GET("/at://:account/:nsid/:rkey", server.WebRecordURI)

	log.Infow("starting server", "bind", httpAddress)
	return e.Start(httpAddress)
}

func customHTTPErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
	}
	c.Logger().Error(err)
	data := pongo2.Context{
		"statusCode": code,
	}
	c.Render(code, "error.html", data)
}

func (srv *Server) WebHome(c echo.Context) error {
	data := pongo2.Context{}
	return c.Render(http.StatusOK, "home.html", data)
}

func (srv *Server) WebPost(c echo.Context) error {
	data := pongo2.Context{}
	handle := c.Param("handle")
	rkey := c.Param("rkey")
	// sanity check argument
	if len(handle) > 4 && len(handle) < 128 && len(rkey) > 0 {
		ctx := c.Request().Context()
		// requires two fetches: first fetch profile (!)
		pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle)
		if err != nil {
			log.Warnf("failed to fetch handle: %s\t%v", handle, err)
			// TODO: only if "not found"
			return echo.NewHTTPError(404, "handle not found: %s", handle)
		} else {
			did := pv.Did
			data["did"] = did

			// then fetch the post thread (with extra context)
			uri := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, rkey)
			tpv, err := appbsky.FeedGetPostThread(ctx, srv.xrpcc, 1, uri)
			if err != nil {
				log.Warnf("failed to fetch post: %s\t%v", uri, err)
				// TODO: only if "not found"
				return echo.NewHTTPError(404, "post not found: %s", handle)
			} else {
				req := c.Request()
				postView := tpv.Thread.FeedDefs_ThreadViewPost.Post
				data["postView"] = postView
				data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
				if postView.Embed != nil && postView.Embed.EmbedImages_View != nil {
					data["imgThumbUrl"] = postView.Embed.EmbedImages_View.Images[0].Thumb
				}
			}
		}

	}
	return c.Render(http.StatusOK, "post.html", data)
}

func (srv *Server) WebProfile(c echo.Context) error {
	data := pongo2.Context{}
	handle := c.Param("handle")
	// sanity check argument
	if len(handle) > 4 && len(handle) < 128 {
		ctx := c.Request().Context()
		pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle)
		if err != nil {
			log.Warnw("failed to fetch handle", "handle", handle, "err", err)
			// TODO: only if "not found"
			return echo.NewHTTPError(404, "handle not found: %s", handle)
		} else {
			req := c.Request()
			data["profileView"] = pv
			data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
		}
		af, err := appbsky.FeedGetAuthorFeed(ctx, srv.xrpcc, handle, "", 100)
		if err != nil {
			log.Warnw("failed to fetch author feed", "handle", handle, "err", err)
			// TODO: show some error?
		} else {
			data["authorFeed"] = af.Feed
			//log.Warnw("author feed", "feed", af.Feed)
		}
	}

	return c.Render(http.StatusOK, "profile.html", data)
}
