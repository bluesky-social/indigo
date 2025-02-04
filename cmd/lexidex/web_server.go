package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
	"github.com/urfave/cli/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed static/*
var StaticFS embed.FS

type WebServer struct {
	echo  *echo.Echo
	httpd *http.Server
	db    *gorm.DB
	dir   identity.Directory
}

func serve(cctx *cli.Context) error {
	debug := cctx.Bool("debug")
	httpAddress := cctx.String("bind")
	db, err := gorm.Open(sqlite.Open(cctx.String("sqlite-path")))
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	e := echo.New()

	// httpd
	var (
		httpTimeout        = 1 * time.Minute
		httpMaxHeaderBytes = 1 * (1024 * 1024)
	)

	srv := &WebServer{
		echo: e,
		db:   db,
		dir:  identity.DefaultDirectory(),
	}
	srv.httpd = &http.Server{
		Handler:        srv,
		Addr:           httpAddress,
		WriteTimeout:   httpTimeout,
		ReadTimeout:    httpTimeout,
		MaxHeaderBytes: httpMaxHeaderBytes,
	}

	e.HideBanner = true
	e.Use(slogecho.New(slog.Default()))
	e.Use(middleware.Recover())
	e.Use(middleware.BodyLimit("64M"))
	e.HTTPErrorHandler = srv.errorHandler
	e.Renderer = NewRenderer("templates/", &TemplateFS, debug)
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

	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", staticHandler)))
	e.GET("/_health", srv.HandleHealthCheck)

	// basic static routes
	e.GET("/robots.txt", echo.WrapHandler(staticHandler))
	e.GET("/favicon.ico", echo.WrapHandler(staticHandler))

	// actual content
	e.GET("/", srv.WebHome)
	e.GET("/query", srv.WebQuery)
	e.GET("/recent", srv.WebRecent)
	e.GET("/domain/:domain", srv.WebDomain)
	e.GET("/lexicon/:nsid", srv.WebLexicon)
	e.GET("/lexicon/:nsid/history", srv.WebLexiconHistory)
	// TODO: e.GET("/lexicon/:nsid/def/:name", srv.WebLexiconDef)

	e.GET("/demo/record", srv.WebDemoRecord)
	e.GET("/demo/query", srv.WebDemoQuery)

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

func (srv *WebServer) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	var errorMessage string
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		errorMessage = fmt.Sprintf("%s", he.Message)
	}
	if code >= 500 {
		slog.Warn("lexidex-http-internal-error", "err", err)
	}
	data := pongo2.Context{
		"statusCode":   code,
		"errorMessage": errorMessage,
	}
	if !c.Response().Committed {
		c.Render(code, "error.html", data)
	}
}

func (srv *WebServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	srv.echo.ServeHTTP(rw, req)
}

func (srv *WebServer) Shutdown() error {
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.httpd.Shutdown(ctx)
}

func (s *WebServer) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "lexidex"})
}
