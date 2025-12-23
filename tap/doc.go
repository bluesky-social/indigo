// Package tap provides a client for consuming atproto events from a tap websocket.
//
// The client handles connection management, automatic reconnection with backoff,
// and optional message acknowledgements.
//
// Basic usage:
//
//	handler := func(ctx context.Context, ev *tap.Event) error {
//		switch payload := ev.Payload().(type) {
//		case *tap.RecordEvent:
//			fmt.Printf("record.Action: %s\n", payload.Action)
//			fmt.Printf("record.Collection: %s\n", payload.Collection)
//		case *tap.IdentityEvent:
//			fmt.Printf("identity.DID: %s\n", payload.DID)
//			fmt.Printf("identity.Handle: %s\n", payload.Handle)
//		}
//		return nil
//	}
//
//	ws, err := tap.NewWebsocket("wss://example.com/tap", handler,
//		tap.WithLogger(slog.Default()),
//		tap.WithAcks(),
//	)
//	if err != nil {
//		// handle error...
//	}
//
//	if err := ws.Run(ctx); err != nil {
//		// handle error...
//	}
//
// Returning an error from the handler will cause the message to be retried with
// exponential backoff. To skip retries for permanent failures, wrap the error
// with [NewNonRetryableError].
package tap
