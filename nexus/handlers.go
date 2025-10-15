package main

import (
	"net/http"

	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (n *Nexus) registerRoutes() {
	n.echo.GET("/health", n.handleHealthcheck)
	n.echo.GET("/listen", n.handleListen)
	n.echo.POST("/add-dids", n.handleAddDids)
}

func (n *Nexus) handleHealthcheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (n *Nexus) handleListen(c echo.Context) error {
	if n.outbox.mode == OutboxModeWebhook {
		return echo.NewHTTPError(http.StatusBadRequest, "websocket not available in webhook mode")
	}

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
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
			if n.outbox.mode == OutboxModeWebsocketAck {
				n.outbox.AckEvent(msg.ID)
			}
		}
	}()

	for {
		select {
		case <-disconnected:
			n.logger.Info("websocket disconnected")
			return nil
		case evt, ok := <-n.outbox.events:
			if !ok {
				return nil
			}
			if err := ws.WriteJSON(evt); err != nil {
				n.logger.Info("websocket write error", "error", err)
				return err
			}
			// In fire-and-forget mode, ack immediately after write succeeds
			// In websocket-ack mode, wait for client to send ack and handle in read loop
			if n.outbox.mode == OutboxModeFireAndForget {
				n.outbox.AckEvent(evt.ID)
			}
		}
	}
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (n *Nexus) handleAddDids(c echo.Context) error {
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
