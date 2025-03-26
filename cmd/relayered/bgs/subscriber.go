package bgs

import (
	"sync"

	"github.com/bluesky-social/indigo/cmd/relayered/stream"

	"github.com/prometheus/client_golang/prometheus"
)

type Subscriber struct {
	outgoing chan *stream.XRPCStreamEvent

	filter func(*stream.XRPCStreamEvent) bool

	done chan struct{}

	cleanup func()

	lk        sync.Mutex
	cleanedUp bool

	ident            string
	enqueuedCounter  prometheus.Counter
	broadcastCounter prometheus.Counter
}
