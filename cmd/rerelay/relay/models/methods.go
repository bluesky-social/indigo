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
