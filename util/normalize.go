package util

import (
	"fmt"
	"strings"
)

func NormalizeHostname(hostname string) (string, error) {
	if len(hostname) > 255 {
		return "", fmt.Errorf("hostname cannot be longer than 255 characters")
	}

	hostname = strings.TrimSpace(hostname)
	hostname = strings.Trim(hostname, ".")

	if len(hostname) == 0 {
		return "", fmt.Errorf("hostname cannot be empty")
	}

	// Lowercase the hostname, as domain names are case-insensitive
	hostname = strings.ToLower(hostname)

	// TODO: other normalizations

	return hostname, nil
}
