package oauth

import (
	"errors"
	"fmt"
)

// ErrGrantRevoked identifies an invalid_grant response: the session has ended
// (for example by revocation or expiry) and a new user login is required.
var ErrGrantRevoked = errors.New("OAuth grant is no longer valid")

// TokenEndpointError distinguishes OAuth refusals from transport failures.
// StatusCode is zero for a transport failure, whose cause is available through
// errors.Is/As (including context deadlines and net.Error). Code is a protocol
// error code, never an error_description or response body containing secrets.
// Only invalid_grant establishes that the grant has ended.
type TokenEndpointError struct {
	StatusCode int
	Code       string
	Err        error
}

func (e *TokenEndpointError) Error() string {
	if e.StatusCode == 0 {
		return "OAuth token endpoint transport failure"
	}
	return fmt.Sprintf("OAuth token endpoint refused request (HTTP %d, %s)", e.StatusCode, e.Code)
}

func (e *TokenEndpointError) Unwrap() error { return e.Err }
func (e *TokenEndpointError) Is(target error) bool {
	return target == ErrGrantRevoked && e.Code == "invalid_grant"
}

func authErrorCode(code string) string {
	switch code {
	case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client", "unsupported_grant_type", "invalid_scope", "use_dpop_nonce", "invalid_dpop_proof", "server_error", "temporarily_unavailable", "access_denied":
		return code
	default:
		return "unknown"
	}
}
