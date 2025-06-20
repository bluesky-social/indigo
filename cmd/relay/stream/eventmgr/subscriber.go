package eventmgr

import (
	"sync"

	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/stream"

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
