package labeling

import (
	"fmt"
	"strconv"

	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func (s *Server) EventsLabelsWebsocket(c echo.Context) error {
	var since *int64
	if sinceVal := c.QueryParam("cursor"); sinceVal != "" {
		sval, err := strconv.ParseInt(sinceVal, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}

	ctx := c.Request().Context()

	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	evts, cancel, err := s.levents.Subscribe(ctx, func(evt *events.LabelStreamEvent) bool {
		return true
	}, since)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Op: events.LEvtKindLabelBatch}
	for {
		select {
		case evt := <-evts:
			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return err
			}

			var obj lexutil.CBOR

			switch {
			case evt.Batch != nil:
				header.Op = events.LEvtKindLabelBatch
				obj = evt.Batch
			case evt.Error != nil:
				header.Op = events.LEvtKindErrorFrame
				obj = evt.Error
			case evt.Info != nil:
				header.Op = events.LEvtKindInfoFrame
				obj = evt.Info
			default:
				return fmt.Errorf("unrecognized event kind")
			}

			if err := header.MarshalCBOR(wc); err != nil {
				return fmt.Errorf("failed to write header: %w", err)
			}

			if err := obj.MarshalCBOR(wc); err != nil {
				return fmt.Errorf("failed to write event: %w", err)
			}

			if err := wc.Close(); err != nil {
				return fmt.Errorf("failed to flush-close our event write: %w", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}
