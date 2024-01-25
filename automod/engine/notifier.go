package engine

import (
	"context"
)

// Interface for a type that can handle sending notifications
type Notifier interface {
	SendAccount(ctx context.Context, service string, c *AccountContext) error
	SendRecord(ctx context.Context, service string, c *RecordContext) error
}
