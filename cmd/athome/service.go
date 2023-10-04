package main

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"time"
	"embed"
	"os/signal"
	"errors"
	"syscall"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/bluesky-social/indigo/util"

	"github.com/urfave/cli/v2"
	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/echo-contrib/echoprometheus"
	slogecho "github.com/samber/slog-echo"
)

//go:embed static/*
var StaticFS embed.FS

type Server struct {
	echo            *echo.Echo
	httpd           *http.Server
	xrpcc *xrpc.Client
}

func serve(cctx *cli.Context) error {
	debug := cctx.Bool("debug")
	httpAddress := cctx.String("bind")
	appviewHost := cctx.String("appview-host")

	xrpcc := &xrpc.Client{
		Client: util.RobustHTTPClient(),
		Host:   appviewHost,
		// Headers: version
	}
	e := echo.New()

	// httpd
	var (
		httpTimeout        = 1 * time.Minute
		httpMaxHeaderBytes = 1 * (1024 * 1024)
	)

	srv := &Server{
		echo:  e,
		xrpcc: xrpcc,
	}
	srv.httpd = &http.Server{
		Handler:        srv,
		Addr:           httpAddress,
		WriteTimeout:   httpTimeout,
		ReadTimeout:    httpTimeout,
		MaxHeaderBytes: httpMaxHeaderBytes,
	}

	e.HideBanner = true
	e.Use(slogecho.New(slog))
	e.Use(middleware.Recover())
	e.Use(echoprometheus.NewMiddleware("athome"))
	e.Use(middleware.BodyLimit("64M"))
	e.HTTPErrorHandler = srv.errorHandler
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "SAMEORIGIN",
		HSTSMaxAge:         31536000, // 365 days
		// TODO:
		// ContentSecurityPolicy
		// XSSProtection
	}))

	// redirect trailing slash to non-trailing slash.
	// all of our current endpoints have no trailing slash.
	e.Use(middleware.RemoveTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		RedirectCode: http.StatusFound,
	}))

	staticHandler := http.FileServer(func() http.FileSystem {
		if debug {
			return http.FS(os.DirFS("static"))
		}
		fsys, err := fs.Sub(StaticFS, "static")
		if err != nil {
			slog.Error("static template error", "err", err)
			os.Exit(-1)
		}
		return http.FS(fsys)
	}())

	e.Renderer = NewRenderer("templates/", &TemplateFS, debug)
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", staticHandler)))

	e.GET("/_health", srv.HandleHealthCheck)
	e.GET("/metrics", echoprometheus.NewHandler())

	// basic static routes
	e.GET("/robots.txt", echo.WrapHandler(staticHandler))
	e.GET("/favicon.ico", echo.WrapHandler(staticHandler))
	e.GET("/", srv.WebHome)

	// generic routes
	//e.GET("/search", srv.WebGeneric)
	//e.GET("/support", srv.WebGeneric)
	//e.GET("/support/privacy", srv.WebGeneric)
	//e.GET("/support/tos", srv.WebGeneric)
	//e.GET("/support/community-guidelines", srv.WebGeneric)
	//e.GET("/support/copyright", srv.WebGeneric)

	// profile endpoints; only first populates info
	e.GET("/profile/:handle", srv.WebProfile)
	//e.GET("/profile/:handle/repo.car.gz", srv.WebProfile)
	//e.GET("/profile/:handle/follows", srv.WebGeneric)
	//e.GET("/profile/:handle/followers", srv.WebGeneric)

	// post endpoints; only first populates info
	e.GET("/profile/:handle/post/:rkey", srv.WebPost)
	//e.GET("/profile/:handle/post/:rkey/liked-by", srv.WebGeneric)
	//e.GET("/profile/:handle/post/:rkey/reposted-by", srv.WebGeneric)

	// feeds
	//e.GET("/feed/:name", srv.WebFeed)

	// redirect
	//e.GET("/at://:account", srv.WebAccountURI)
	//e.GET("/at://:account/:nsid/:rkey", srv.WebRecordURI)

	// Start the server
	slog.Info("starting server", "bind", httpAddress)
	go func() {
		if err := srv.httpd.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("HTTP server shutting down unexpectedly", "err", err)
			}
		}
	}()

	// Wait for a signal to exit.
	slog.Info("registering OS exit signal handler")
	quit := make(chan struct{})
	exitSignals := make(chan os.Signal, 1)
	signal.Notify(exitSignals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-exitSignals
		slog.Info("received OS exit signal", "signal", sig)

		// Shut down the HTTP server
		if err := srv.Shutdown(); err != nil {
			slog.Error("HTTP server shutdown error", "err", err)
		}

		// Trigger the return that causes an exit.
		close(quit)
	}()
	<-quit
	slog.Info("graceful shutdown complete")
	return nil
}

type GenericStatus struct {
	Daemon  string `json:"daemon"`
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (srv *Server) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
	}
	if code >= 500 {
		slog.Warn("abyss-http-internal-error", "err", err)
	}
	data := pongo2.Context{
		"statusCode": code,
	}
	c.Render(code, "error.html", data)
}

func (srv *Server) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	srv.echo.ServeHTTP(rw, req)
}

func (srv *Server) Shutdown() error {
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.httpd.Shutdown(ctx)
}

func (s *Server) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "abyss"})
}

