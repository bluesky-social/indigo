package main

import (
	"context"
	"io"
	"os"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func resolveIdent(ctx context.Context, arg string) (*identity.Identity, error) {
	id, err := syntax.ParseAtIdentifier(arg)
	if err != nil {
		return nil, err
	}

	dir := identity.DefaultDirectory()
	return dir.Lookup(ctx, *id)
}

const stdIOPath = "-"

func getFileOrStdin(path string) (io.Reader, error) {
	if path == stdIOPath {
		return os.Stdin, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func getFileOrStdout(path string) (io.WriteCloser, error) {
	if path == stdIOPath {
		return os.Stdout, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, err
	}
	return file, nil
}
