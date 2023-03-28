package pds

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"

	"strings"

	"golang.org/x/crypto/scrypt"
)

// these constants have the same values as bluesky-social/atproto/packages/pds/src/db/scrypt.ts
const (
	cost            = 16384
	blockSize       = 8
	parallelization = 1
	keylen          = 64
)

var ErrInvalidUsernameOrPassword = fmt.Errorf("invalid username or password")

func encodePassword(password string) (string, error) {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}
	salt := hex.EncodeToString(buf)
	dk, err := scrypt.Key([]byte(password), []byte(salt), cost, blockSize, parallelization, keylen)
	if err != nil {
		return "", err
	}
	return salt + ":" + hex.EncodeToString(dk), nil
}

func verifyPassword(storedHash, password string) error {
	parts := strings.Split(storedHash, ":")
	if len(parts) != 2 {
		return ErrInvalidUsernameOrPassword
	}
	salt := parts[0]
	passwordHashed := parts[1]
	dk, err := scrypt.Key([]byte(password), []byte(salt), cost, blockSize, parallelization, keylen)
	if err != nil {
		return err
	}
	dst := make([]byte, hex.EncodedLen(len(dk)))
	hex.Encode(dst, dk)

	if subtle.ConstantTimeCompare([]byte(passwordHashed), dst) != 1 {
		return ErrInvalidUsernameOrPassword
	}
	return nil
}
