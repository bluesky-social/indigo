package main

import (
	"net/http"

	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm/clause"
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

	for op := range n.outbox.outCh {
		if err := ws.WriteJSON(op); err != nil {
			n.logger.Info("websocket write error", "error", err)
			return nil
		}
	}
	return nil
}

type DidPayload struct {
	DIDs []string `json:"dids"`
}

func (n *Nexus) handleAddDids(c echo.Context) error {
	var payload DidPayload
	if err := c.Bind(&payload); err != nil {
		return err
	}

	rows := make([]models.FilterDid, 0, len(payload.DIDs))
	for _, did := range payload.DIDs {
		rows = append(rows, models.FilterDid{Did: did})
	}

	err := n.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
	if err != nil {
		n.logger.Error("failed to insert dids", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	for _, did := range payload.DIDs {
		n.filterDids[did] = true
	}

	return c.NoContent(http.StatusOK)
}
