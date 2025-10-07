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
	return c.JSON(200, map[string]string{
		"status": "ok",
	})
}

func (n *Nexus) handleListen(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	n.logger.Info("websocket connected")

	return n.outbox.Subscribe(c.Request().Context(), func(op *Op) error {
		return ws.WriteJSON(op)
	})
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (n *Nexus) handleAddDids(c echo.Context) error {
	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	filterDids := make([]models.FilterDid, len(payload.DIDs))
	for i, did := range payload.DIDs {
		filterDids[i] = models.FilterDid{
			Did:   did,
			State: models.RepoStatePending,
		}
	}

	if err := n.db.Save(&filterDids).Error; err != nil {
		n.logger.Error("failed to upsert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	n.mu.Lock()
	for _, did := range payload.DIDs {
		n.filterDids[did] = true
	}
	n.mu.Unlock()

	for _, did := range payload.DIDs {
		n.queueBackfill(did)
	}

	n.logger.Info("added dids and queued backfills", "count", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"count": len(payload.DIDs),
	})
}
