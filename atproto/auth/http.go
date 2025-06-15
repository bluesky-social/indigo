package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// HTTP Middleware for atproto admin auth, which is HTTP Basic auth with the username "admin".
//
// This supports multiple admin passwords, which makes it easier to rotate service secrets.
//
// This can be used with `echo.WrapMiddleware` (part of the echo web framework)
func AdminAuthMiddleware(handler http.HandlerFunc, adminPasswords []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok && username == "admin" {
			for _, pw := range adminPasswords {
				if subtle.ConstantTimeCompare([]byte(pw), []byte(password)) == 1 {
					handler(w, r)
					return
				}
			}
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="admin", charset="UTF-8"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Unauthorized",
			"message": "atproto admin auth required, but missing or incorrect password",
		})
	}
}

// HTTP Middleware for inter-service auth, which is HTTP Bearer with JWT.
//
// 'mandatory' indicates whether valid inter-service auth must be present, or just optional.
func (v *ServiceAuthValidator) Middleware(handler http.HandlerFunc, mandatory bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		if hdr := r.Header.Get("Authorization"); hdr != "" {
			parts := strings.Split(hdr, " ")
			if parts[0] != "Bearer" || len(parts) != 2 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "Unauthorized",
					"message": "atproto service auth required, but missing or incorrect formatting",
				})
				return
			}

			var lxm *syntax.NSID
			uparts := strings.Split(r.URL.Path, "/")
			// TODO: should this "fail closed"? eg, reject if not a valid XRPC endpoint
			if len(uparts) >= 3 && uparts[1] == "xrpc" {
				nsid, err := syntax.ParseNSID(uparts[2])
				if nil == err {
					lxm = &nsid
				}
			}

			did, err := v.Validate(r.Context(), parts[1], lxm)
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "Unauthorized",
					"message": fmt.Sprintf("invalid service auth: %s", err),
				})
				return
			}
			ctx := context.WithValue(r.Context(), "did", did)
			handler(w, r.WithContext(ctx))
			return
		}

		if mandatory {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Unauthorized",
				"message": "atproto service auth required",
			})
			return
		}
		handler(w, r)
	}
}
