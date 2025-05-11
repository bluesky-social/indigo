package oauth

import (
	"math/rand"
	"encoding/base64"
)

func randomNonce() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
