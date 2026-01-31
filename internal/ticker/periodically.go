package ticker

import (
	"context"
	"fmt"
	"time"
)

// Periodically runs the provided task function at the specified interval until the context is done or an error occurs.
func Periodically(ctx context.Context, interval time.Duration, task func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := task(ctx); err != nil {
				return fmt.Errorf("periodic task failed: %w", err)
			}
		}
	}
}
