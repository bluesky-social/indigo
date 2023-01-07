package schemagen

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/whyrusleeping/go-did"
	"github.com/whyrusleeping/gosky/key"
	"gorm.io/gorm"
)

type FakeDidMapping struct {
	gorm.Model
	Handle string
	Did    string `gorm:"index"`
}

type FakeDid struct {
	db *gorm.DB
}

func NewFakeDid(db *gorm.DB) *FakeDid {
	db.AutoMigrate(&FakeDidMapping{})
	return &FakeDid{db}
}

func (fd *FakeDid) GetDocument(ctx context.Context, did string) (*did.Document, error) {
	panic("nyi")
}

func (fd *FakeDid) CreateDID(ctx context.Context, sigkey *key.Key, recovery string, handle string, service string) (string, error) {
	buf := make([]byte, 8)
	rand.Read(buf)
	d := "did:plc:" + hex.EncodeToString(buf)

	if err := fd.db.Create(&FakeDidMapping{
		Handle: handle,
		Did:    d,
	}).Error; err != nil {
		return "", err
	}

	return d, nil
}
