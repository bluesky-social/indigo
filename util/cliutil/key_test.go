package cliutil

import (
	"os"
	"path/filepath"
	"testing"
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
	_, err = LoadKeyFromFile(fkey)
	if err != nil {
		t.Fatal(err)
	}
}
