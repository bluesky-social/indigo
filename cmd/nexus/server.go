package main

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type NexusServer struct {
	db     *gorm.DB
	echo   *echo.Echo
	logger *slog.Logger
	Outbox *Outbox
}

func (ns *NexusServer) Start(address string) error {
	ns.echo = echo.New()
	ns.echo.HideBanner = true
	ns.echo.GET("/health", ns.handleHealthcheck)
	ns.echo.GET("/channel", ns.handleChannelWebsocket)
	ns.echo.POST("/add-repos", ns.handleAddRepos)
	ns.echo.POST("/remove-repos", ns.handleRemoveRepos)
	ns.echo.Any("/debug/pprof/*", echo.WrapHandler(http.DefaultServeMux))
	return ns.echo.Start(address)
}

// Shutdown gracefully shuts down the HTTP server.
func (ns *NexusServer) Shutdown(ctx context.Context) error {
	return ns.echo.Shutdown(ctx)
}

func (ns *NexusServer) handleHealthcheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (ns *NexusServer) handleChannelWebsocket(c echo.Context) error {
	if ns.Outbox.mode == OutboxModeWebhook {
		return echo.NewHTTPError(http.StatusBadRequest, "websocket not available in webhook mode")
	}

	ws, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	ns.logger.Info("websocket connected")

	// read loop to detect disconnects and handle acks in websocket-ack mode
	disconnected := make(chan struct{})

	go func() {
		for {
			var msg AckMessage
			if err := ws.ReadJSON(&msg); err != nil {
				close(disconnected)
				return
			}

			// Process acks directly in websocket-ack mode
			if ns.Outbox.mode == OutboxModeWebsocketAck {
				go ns.Outbox.AckEvent(msg.ID)
			}
		}
	}()

	for {
		select {
		case <-disconnected:
			ns.logger.Info("websocket disconnected")
			return nil
		case msg, ok := <-ns.Outbox.events:
			if !ok {
				return nil
			}
			if err := ws.WriteMessage(websocket.TextMessage, msg.JSON); err != nil {
				ns.logger.Info("websocket write error", "error", err)
				return nil
			}
			// In fire-and-forget mode, ack immediately after write succeeds
			// In websocket-ack mode, wait for client to send ack and handle in read loop
			if ns.Outbox.mode == OutboxModeFireAndForget {
				go ns.Outbox.AckEvent(msg.ID)
			}
		}
	}
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (ns *NexusServer) handleAddRepos(c echo.Context) error {
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

	if err := ns.db.Save(&dids).Error; err != nil {
		ns.logger.Error("failed to upsert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	ns.logger.Info("added dids", "count", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"count": len(payload.DIDs),
	})
}

func (ns *NexusServer) handleRemoveRepos(c echo.Context) error {
	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	for _, did := range payload.DIDs {
		err := deleteRepo(ns.db, did)
		if err != nil {
			ns.logger.Error("failed to delete repo", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
	}

	ns.logger.Info("removed dids", "count", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"count": len(payload.DIDs),
	})
}
