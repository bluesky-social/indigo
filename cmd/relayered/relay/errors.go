package relay

import (
	"errors"
)

var (
	ErrHostNotFound        = errors.New("unknown host or PDS")
	ErrAccountNotFound     = errors.New("unknown account")
	ErrAccountRepoNotFound = errors.New("repository state not available")
)
