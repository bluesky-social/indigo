package relay

import (
	"errors"
)

var (
	ErrHostNotFound        = errors.New("unknown host or PDS")
	ErrAccountNotFound     = errors.New("unknown account")
	ErrAccountRepoNotFound = errors.New("repository state not available")

	// TODO: these might need better names
	ErrTimeoutShutdown    = errors.New("timed out waiting for new events")
	ErrNewSubsDisabled    = errors.New("new subscriptions temporarily disabled")
	ErrNoActiveConnection = errors.New("no active connection to host")
)
