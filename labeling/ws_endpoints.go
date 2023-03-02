package labeling

import (
	"fmt"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/pds"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func (s *Server) EventsLabelsWebsocket(c echo.Context) error {
	did := c.Request().Header.Get("DID")
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	var peering pds.Peering
	if did != "" {
		if err := s.db.First(&peering, "did = ?", did).Error; err != nil {
			return err
		}
	}

	evts, cancel, err := s.levents.Subscribe(func(evt *events.LabelEvent) bool {
		return true
	}, nil)
	if err != nil {
		return err
	}
	defer cancel()

	// TODO: Op: events.EvtKindLabel
	header := events.EventHeader{Op: events.EvtKindRepoAppend}
	for evt := range evts {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		if err := header.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		if err := evt.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write event: %w", err)
		}

		if err := wc.Close(); err != nil {
			return fmt.Errorf("failed to flush-close our event write: %w", err)
		}
	}

	return nil
}
