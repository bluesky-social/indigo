package util

import (
	"strings"
)

// Takes a "host" string and returns an appropriate websocket URL. Defaults to
// wss://, except for localhost. Tries to convert http/https to wss/ws.
func WebsocketUrlForHost(host string) string {
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "wss://") || strings.HasPrefix(host, "ws://") {
		return host
	}
	if strings.HasPrefix(host, "https://") {
		return "wss://" + strings.TrimPrefix(host, "https://")
	}
	if strings.HasPrefix(host, "http://") {
		return "ws://" + strings.TrimPrefix(host, "http://")
	}
	if strings.Contains(host, "://") {
		// don't mess with unexpected methods
		return host
	}
	if strings.HasPrefix(host, "127.0.0.") || strings.HasPrefix(host, "[::1]") {
		return "ws://" + host
	}
	hostname := strings.SplitN(host, ":", 2)[0]
	if hostname == "localhost" {
		return "ws://" + host
	}
	return "wss://" + host
}
