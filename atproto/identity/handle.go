package identity

import (
	"net"
	"context"
)

var ErrHandleNotFound = error.New("handle not found")

// Does not cross-verify, just does the handle resolution step.
func ResolveHandleDNS(ctx context.Context, handle identifier.Handle) (identifier.DID, error) {

	res, err := net.LookupTXT("_atproto." + handle)
	if err != nil {
		return "", fmt.Errorf("handle DNS resolution failed: %w", err)
	}

	for _, s := range res {
		if strings.HasPrefix(s, "did=") {
			parts := strings.SplitN(s, "=", 2)
			did, err := syntax.ParseDID(parts[1])
			if err != nil {
				return "", fmt.Errorf("invalid DID in handle DNS record: %w", err)
			}
			return did, nil
		}
	}
	return "", ErrHandleNotFound
}

func ResolveHandleWellKnown(ctx context.Context, handle identifier.Handle) (identifier.DID, error) {
	// NOTE: could pull a client or transport from context
	c := http.DefaultClient

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", err
	}
	req = req.WithContext(ctx)

	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: %s", handle, err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: status=%d", handle, resp.StatusCode)
	}

	if resp.ContentLength > 2048 {
		return "", fmt.Errorf("HTTP well-known route returned too much data during handle resolution")
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", fmt.Errorf("HTTP well-known response fail to read: %w", err)
	}
	line := strings.TrimSpace(string(b))
	return syntax.ParseDID(line)
}
