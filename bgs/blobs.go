package bgs

import (
	"context"
	"os"
	"path/filepath"
)

type BlobStore interface {
	PutBlob(ctx context.Context, cid string, did string, blob []byte) error
	GetBlob(ctx context.Context, cid string, did string) ([]byte, error)
}

type DiskBlobStore struct {
	Dir string
}

func (dbs *DiskBlobStore) PutBlob(ctx context.Context, cid string, did string, blob []byte) error {
	udir := filepath.Join(dbs.Dir, did)
	if err := os.MkdirAll(udir, 0775); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(udir, cid), blob, 0664)
}

func (dbs *DiskBlobStore) GetBlob(ctx context.Context, cid string, did string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dbs.Dir, did, cid))
}
