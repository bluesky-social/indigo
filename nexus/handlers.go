package main

import (
	"context"
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
	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	n.logger.Info("websocket connected")

	// Subscribe to outbox - it handles draining DB and streaming live events
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

	// Start transaction
	tx := n.db.Begin()
	defer tx.Rollback()

	var newDids []string

	for _, did := range payload.DIDs {
		// Check if already exists
		var existing models.FilterDid
		err := tx.First(&existing, "did = ?", did).Error

		if err == nil {
			// Already exists, skip
			n.logger.Info("did already tracked", "did", did, "state", existing.State)
			continue
		}

		// Add to filter list with pending state
		filterDid := &models.FilterDid{
			Did:   did,
			State: models.RepoStatePending,
		}
		if err := tx.Create(filterDid).Error; err != nil {
			n.logger.Error("failed to insert did", "error", err, "did", did)
			return echo.NewHTTPError(http.StatusInternalServerError)
		}

		// Add to in-memory filter
		n.mu.Lock()
		n.filterDids[did] = true
		n.mu.Unlock()

		newDids = append(newDids, did)
	}

	if err := tx.Commit().Error; err != nil {
		n.logger.Error("failed to commit transaction", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	// Kick off backfill for each new DID
	for _, did := range newDids {
		go n.backfillDid(context.Background(), did)
	}

	n.logger.Info("added dids and started backfills", "new", len(newDids), "total", len(payload.DIDs))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"added":  len(newDids),
		"total":  len(payload.DIDs),
	})
}
