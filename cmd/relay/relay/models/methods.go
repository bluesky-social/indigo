package models

import (
	"fmt"
)

// returns base HTTP URL for the host: scheme, hostname, optional port, no path segment
func (h *Host) BaseURL() string {
	scheme := "https"
	if h.NoSSL {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, h.Hostname)
}

// returns websocket URL for the host: scheme, hostname, optional port, and path.
func (h *Host) SubscribeReposURL() string {
	scheme := "wss"
	if h.NoSSL {
		scheme = "ws"
	}
	return fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos", scheme, h.Hostname)
}

func (a *Account) AccountStatus() AccountStatus {
	if a.Status != AccountStatusActive {
		return a.Status
	}
	return a.UpstreamStatus
}

// Returns a pointer to a copy of status string; or nil if status is active.
//
// Helpful for account info responses which have a boolean 'active' and optional 'status' field (like the #account message)
func (a *Account) StatusField() *string {
	if a.IsActive() {
		return nil
	}
	s := string(a.AccountStatus())
	return &s
}

func (a *Account) IsActive() bool {
	return (a.Status == AccountStatusActive || a.Status == AccountStatusThrottled) && a.UpstreamStatus == AccountStatusActive
}
