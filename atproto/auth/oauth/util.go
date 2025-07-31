package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// this generates pseudo-unique nonces to prevent token (JWT) replay. these do not need to be cryptographically resilient
func randomNonce() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func s256CodeChallenge(raw string) string {
	b := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func strPtr(raw string) *string {
	return &raw
}
