package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
)

// TODO: longer
func randomNonce() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func S256CodeChallenge(raw string) string {
	b := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(b[:])
}
