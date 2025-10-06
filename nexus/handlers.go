package main

import (
	"encoding/json"
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
	// Check if already connected
	if err := n.outbox.Connect(); err != nil {
		n.logger.Error("connection refused", "error", err)
		return echo.NewHTTPError(http.StatusConflict, "websocket already connected")
	}
	defer n.outbox.Disconnect()

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	n.logger.Info("websocket connected")

	// Send buffered events first
	var bufferedEvts []models.BufferedEvt
	if err := n.db.Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		n.logger.Error("failed to load buffered events", "error", err)
		return err
	}

	if len(bufferedEvts) > 0 {
		n.logger.Info("draining buffered events", "count", len(bufferedEvts))
		for _, evt := range bufferedEvts {
			op := &Op{
				Did:        evt.Did,
				Collection: evt.Collection,
				Rkey:       evt.Rkey,
				Action:     evt.Action,
				Cid:        evt.Cid,
			}

			if evt.Record != "" {
				var record map[string]interface{}
				if err := json.Unmarshal([]byte(evt.Record), &record); err != nil {
					n.logger.Error("failed to unmarshal record", "error", err, "id", evt.ID)
					continue
				}
				op.Record = record
			}

			if err := ws.WriteJSON(op); err != nil {
				n.logger.Info("websocket write error", "error", err)
				return nil
			}
		}

		// Delete buffered events
		if err := n.db.Delete(&bufferedEvts).Error; err != nil {
			n.logger.Error("failed to delete buffered events", "error", err)
		} else {
			n.logger.Info("cleared buffered events", "count", len(bufferedEvts))
		}
	}

	// Mark that we're done draining and ready for live events
	n.outbox.StartLive()
	n.logger.Info("starting live event stream")
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
