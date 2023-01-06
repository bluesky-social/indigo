package schemagen

import (
	"crypto/rand"
	"encoding/hex"

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

func (fd *FakeDid) NewForHandle(handle string) (string, error) {
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
