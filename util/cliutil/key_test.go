package cliutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whyrusleeping/go-did"
)

func TestKeyGenerationAndLoading(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "msttest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempdir)
	fkey := filepath.Join(tempdir, "test.key")
	err = GenerateKeyToFile(fkey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := LoadKeyFromFile(fkey)
	if err != nil {
		t.Fatal(err)
	}

	if key.Type != did.KeyTypeP256 {
		t.Fatalf("unexpected type of the key %s", key.KeyType())
	}
}
