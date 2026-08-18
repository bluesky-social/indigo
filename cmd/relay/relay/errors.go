package relay

import (
	"errors"
)

var (
	ErrHostNotFound        = errors.New("unknown host or PDS")
	ErrHostInactive        = errors.New("no active connection to host")
	ErrHostNotPDS          = errors.New("server is not a PDS")
	ErrNewHostsDisabled    = errors.New("new host subscriptions temporarily disabled")
	ErrAccountNotFound     = errors.New("unknown account")
	ErrAccountRepoNotFound = errors.New("repository state not available")
)
