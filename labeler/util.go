package labeler

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/atproto/crypto"
)

// TODO:(bnewbold): duplicates elsewhere; should refactor into cliutil?
func LoadOrCreateKeyFile(kfile, kid string) (*crypto.PrivateKey, error) {
	_, err := os.Stat(kfile)
	if errors.Is(err, os.ErrNotExist) {
		// file doesn't exist; create a new key and write it out, then we will re-read it
		skey, err := crypto.GeneratePrivateKeyP256()
		if err != nil {
			return nil, err
		}

		os.MkdirAll(filepath.Dir(kfile), os.ModePerm)

		err = os.WriteFile(kfile, []byte(skey.Multibase()), 0664)
		return nil, err
	}

	kb, err := os.ReadFile(kfile)
	if err != nil {
		return nil, err
	}

	var sec crypto.PrivateKey
	sec, err = crypto.ParsePrivateMultibase(string(kb))
	if err != nil {
		return nil, err
	}
	return &sec, err
}

func dedupeStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range in {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
