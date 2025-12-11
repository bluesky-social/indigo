package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TapServer struct {
	db            *gorm.DB
	echo          *echo.Echo
	logger        *slog.Logger
	outbox        *Outbox
	adminPassword string
	idDir         identity.Directory
	firehose      *FirehoseProcessor
	crawler       *Crawler
}

func (ts *TapServer) Start(address string) error {
	ts.echo = echo.New()
	ts.echo.HideBanner = true
	ts.echo.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))

	// Apply admin auth middleware if configured
	if ts.adminPassword != "" {
		ts.echo.Use(echo.WrapMiddleware(func(next http.Handler) http.Handler {
			return auth.AdminAuthMiddleware(next.ServeHTTP, []string{ts.adminPassword})
		}))
	}

	ts.echo.GET("/health", ts.handleHealthcheck)
	ts.echo.GET("/channel", ts.handleChannelWebsocket)
	ts.echo.POST("/repos/add", ts.handleAddRepos)
	ts.echo.POST("/repos/remove", ts.handleRemoveRepos)
	ts.echo.GET("/resolve/:did", ts.handleResolveDID)
	ts.echo.GET("/info/:did", ts.handleInfoRepo)
	ts.echo.GET("/stats/repo-count", ts.handleStatsRepoCount)
	ts.echo.GET("/stats/record-count", ts.handleStatsRecordCount)
	ts.echo.GET("/stats/outbox-buffer", ts.handleStatsOutboxBuffer)
	ts.echo.GET("/stats/resync-buffer", ts.handleStatsResyncBuffer)
	ts.echo.GET("/stats/cursors", ts.handleStatsCursors)
	return ts.echo.Start(address)
}

// Shutdown gracefully shuts down the HTTP server.
func (ts *TapServer) Shutdown(ctx context.Context) error {
	return ts.echo.Shutdown(ctx)
}

// RunMetrics starts the metrics and pprof server on a separate port.
func (ts *TapServer) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (ts *TapServer) handleHealthcheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (ts *TapServer) handleChannelWebsocket(c echo.Context) error {
	if ts.outbox.mode == OutboxModeWebhook {
		return echo.NewHTTPError(http.StatusBadRequest, "websocket not available in webhook mode")
	}

	ws, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	ts.logger.Info("websocket connected")

	// read loop to detect disconnects and handle acks in websocket-ack mode
	disconnected := make(chan struct{})

	go func() {
		for {
			var msg WsResponse
			if err := ws.ReadJSON(&msg); err != nil {
				close(disconnected)
				return
			}

			if ts.outbox.mode == OutboxModeWebsocketAck && msg.Type == WsResponseAck {
				go ts.outbox.AckEvent(msg.ID)
			}
		}
	}()

	for {
		select {
		case <-disconnected:
			ts.logger.Info("websocket disconnected")
			return nil
		case msg, ok := <-ts.outbox.outgoing:
			if !ok {
				return nil
			}
			if err := ws.WriteMessage(websocket.TextMessage, msg.Event); err != nil {
				ts.logger.Info("websocket write error", "error", err)
				return nil
			}
			// In fire-and-forget mode, ack immediately after write succeeds
			// In websocket-ack mode, wait for client to send ack and handle in read loop
			if ts.outbox.mode == OutboxModeFireAndForget {
				go ts.outbox.AckEvent(msg.ID)
			}
		}
	}
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (ts *TapServer) handleAddRepos(c echo.Context) error {
	ctx := c.Request().Context()

	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	dids := make([]models.Repo, len(payload.DIDs))
	for i, did := range payload.DIDs {
		dids[i] = models.Repo{
			Did:   did,
			State: models.RepoStatePending,
		}
	}

	if err := ts.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&dids).Error; err != nil {
		ts.logger.Error("failed to insert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	ts.logger.Info("added dids", "count", len(payload.DIDs))

	return c.NoContent(http.StatusOK)
}

func (ts *TapServer) handleRemoveRepos(c echo.Context) error {
	ctx := c.Request().Context()

	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	for _, did := range payload.DIDs {
		err := deleteRepo(ts.db.WithContext(ctx), did)
		if err != nil {
			ts.logger.Error("failed to delete repo", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
	}

	ts.logger.Info("removed dids", "count", len(payload.DIDs))

	return c.NoContent(http.StatusOK)
}

func (ts *TapServer) handleResolveDID(c echo.Context) error {
	didParam := c.Param("did")

	did, err := syntax.ParseDID(didParam)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "improperly formatted DID")
	}

	ident, err := ts.idDir.LookupDID(c.Request().Context(), did)
	if err != nil {
		if err == identity.ErrDIDNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "DID not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve DID")
	}

	return c.JSON(http.StatusOK, ident.DIDDocument())
}

func (ts *TapServer) handleInfoRepo(c echo.Context) error {
	ctx := c.Request().Context()
	did := c.Param("did")

	var repo models.Repo
	if err := ts.db.WithContext(ctx).First(&repo, "did = ?", did).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "repo not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo")
	}

	var recordCount int64
	ts.db.WithContext(ctx).Model(&models.RepoRecord{}).Where("did = ?", did).Count(&recordCount)

	return c.JSON(http.StatusOK, map[string]any{
		"did":     repo.Did,
		"handle":  repo.Handle,
		"state":   repo.State,
		"rev":     repo.Rev,
		"error":   repo.ErrorMsg,
		"retries": repo.RetryCount,
		"records": recordCount,
	})
}

func (ts *TapServer) handleStatsRepoCount(c echo.Context) error {
	ctx := c.Request().Context()
	var count int64
	if err := ts.db.WithContext(ctx).Model(&models.Repo{}).Count(&count).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo count")
	}
	return c.JSON(http.StatusOK, map[string]int64{"repo_count": count})
}

func (ts *TapServer) handleStatsRecordCount(c echo.Context) error {
	ctx := c.Request().Context()
	var count int64
	if err := ts.db.WithContext(ctx).Model(&models.RepoRecord{}).Count(&count).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get record count")
	}
	return c.JSON(http.StatusOK, map[string]int64{"record_count": count})
}

func (ts *TapServer) handleStatsOutboxBuffer(c echo.Context) error {
	ctx := c.Request().Context()
	var count int64
	if err := ts.db.WithContext(ctx).Model(&models.OutboxBuffer{}).Count(&count).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get outbox buffer size")
	}
	return c.JSON(http.StatusOK, map[string]int64{"outbox_buffer": count})
}

func (ts *TapServer) handleStatsResyncBuffer(c echo.Context) error {
	ctx := c.Request().Context()
	var count int64
	if err := ts.db.WithContext(ctx).Model(&models.ResyncBuffer{}).Count(&count).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get resync buffer size")
	}
	return c.JSON(http.StatusOK, map[string]int64{"resync_buffer": count})
}

type CursorsResp struct {
	Firehose  *int64  `json:"firehose,omitempty"`
	ListRepos *string `json:"list_repos,omitempty"`
}

func (ts *TapServer) handleStatsCursors(c echo.Context) error {
	ctx := c.Request().Context()
	resp := CursorsResp{}

	if ts.firehose != nil {
		seq := ts.firehose.lastSeq.Load()
		resp.Firehose = &seq
	}

	// Get enumeration cursor based on crawler config
	if ts.crawler != nil {
		cursor, err := ts.crawler.GetCursor(ctx)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get list repos cursor")
		}
		resp.ListRepos = &cursor
	}

	return c.JSON(http.StatusOK, resp)
}
