package store

import "github.com/bluesky-social/indigo/cmd/butterfly/remote"

type Store interface {
	// Initialize the store
	Setup() error

	// Teardown the store
	Close() error

	// Subscribe to a record emitter
	Receive(s *remote.RemoteStream) error
}
