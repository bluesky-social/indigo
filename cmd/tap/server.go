package main

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type TapServer struct {
	db     *gorm.DB
	echo   *echo.Echo
	logger *slog.Logger
	outbox *Outbox
}

func (ts *TapServer) Start(address string) error {
	ts.echo = echo.New()
	ts.echo.HideBanner = true
	ts.echo.GET("/health", ts.handleHealthcheck)
	ts.echo.GET("/channel", ts.handleChannelWebsocket)
	ts.echo.POST("/add-repos", ts.handleAddRepos)
	ts.echo.POST("/remove-repos", ts.handleRemoveRepos)
	ts.echo.Any("/debug/pprof/*", echo.WrapHandler(http.DefaultServeMux))
	return ts.echo.Start(address)
}

// Shutdown gracefully shuts down the HTTP server.
func (ts *TapServer) Shutdown(ctx context.Context) error {
	return ts.echo.Shutdown(ctx)
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

	if err := ts.db.Save(&dids).Error; err != nil {
		ts.logger.Error("failed to upsert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	ts.logger.Info("added dids", "count", len(payload.DIDs))

	return c.NoContent(http.StatusOK)
}

func (ts *TapServer) handleRemoveRepos(c echo.Context) error {
	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	for _, did := range payload.DIDs {
		err := deleteRepo(ts.db, did)
		if err != nil {
			ts.logger.Error("failed to delete repo", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
	}

	ts.logger.Info("removed dids", "count", len(payload.DIDs))

	return c.NoContent(http.StatusOK)
}
