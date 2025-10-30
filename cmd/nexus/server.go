package main

import (
	"context"
	"log/slog"
	"net/http"

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

func (n *NexusServer) Start(address string) error {
	n.echo = echo.New()
	n.echo.HideBanner = true
	n.echo.GET("/health", n.handleHealthcheck)
	n.echo.GET("/listen", n.handleListen)
	n.echo.POST("/add-repos", n.handleAddRepos)
	n.echo.POST("/remove-repos", n.handleAddRepos)
	return n.echo.Start(address)
}

func (n *NexusServer) Shutdown(ctx context.Context) error {
	return n.echo.Shutdown(ctx)
}

func (n *NexusServer) handleHealthcheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (n *NexusServer) handleListen(c echo.Context) error {
	if n.Outbox.mode == OutboxModeWebhook {
		return echo.NewHTTPError(http.StatusBadRequest, "websocket not available in webhook mode")
	}

	ws, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	n.logger.Info("websocket connected")

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
			if n.Outbox.mode == OutboxModeWebsocketAck {
				n.Outbox.AckEvent(msg.ID)
			}
		}
	}()

	for {
		select {
		case <-disconnected:
			n.logger.Info("websocket disconnected")
			return nil
		case evt, ok := <-n.Outbox.events:
			if !ok {
				return nil
			}
			if err := ws.WriteJSON(evt); err != nil {
				n.logger.Info("websocket write error", "error", err)
				return nil
			}
			// In fire-and-forget mode, ack immediately after write succeeds
			// In websocket-ack mode, wait for client to send ack and handle in read loop
			if n.Outbox.mode == OutboxModeFireAndForget {
				n.Outbox.AckEvent(evt.ID)
			}
		}
	}
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (n *NexusServer) handleAddRepos(c echo.Context) error {
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

	if err := n.db.Save(&dids).Error; err != nil {
		n.logger.Error("failed to upsert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	n.logger.Info("added dids", "count", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"count": len(payload.DIDs),
	})
}

func (n *NexusServer) handleRemoveRepos(c echo.Context) error {
	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	for _, did := range payload.DIDs {
		err := deleteRepo(n.db, did)
		if err != nil {
			n.logger.Error("failed to delete repo", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
	}

	n.logger.Info("added dids", "count", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"count": len(payload.DIDs),
	})
}
