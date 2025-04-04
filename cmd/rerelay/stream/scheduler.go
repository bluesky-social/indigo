package stream

import (
	"context"
)

type Scheduler interface {
	AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error
	Shutdown()
}
