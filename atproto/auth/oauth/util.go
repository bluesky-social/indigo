package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// This is used both for PKCE challenges, and for pseudo-unique nonces to prevent token (JWT) replay.
func secureRandomBase64(sizeBytes uint) string {
	buf := make([]byte, sizeBytes)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// Computes an SHA-256 base64url-encoded challenge string, as used for PKCE.
func S256CodeChallenge(raw string) string {
	b := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func strPtr(raw string) *string {
	return &raw
}
