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
	Handle  string
	Did     string `gorm:"index"`
	Service string
}

type FakeDid struct {
	db *gorm.DB
}

func NewFakeDid(db *gorm.DB) *FakeDid {
	db.AutoMigrate(&FakeDidMapping{})
	return &FakeDid{db}
}

func (fd *FakeDid) GetDocument(ctx context.Context, udid string) (*did.Document, error) {
	var rec FakeDidMapping
	if err := fd.db.First(&rec, "did = ?", udid).Error; err != nil {
		return nil, err
	}

	d, err := did.ParseDID(rec.Did)
	if err != nil {
		panic(err)
	}

	return &did.Document{
		Context: []string{},

		ID: d,

		AlsoKnownAs: []string{"https://" + rec.Handle},

		//Authentication []interface{} `json:"authentication"`

		//VerificationMethod []VerificationMethod `json:"verificationMethod"`

		Service: []did.Service{
			did.Service{
				//ID:              "",
				Type:            "pds",
				ServiceEndpoint: "http://" + rec.Service,
			},
		},
	}, nil
}

func (fd *FakeDid) CreateDID(ctx context.Context, sigkey *key.Key, recovery string, handle string, service string) (string, error) {
	buf := make([]byte, 8)
	rand.Read(buf)
	d := "did:plc:" + hex.EncodeToString(buf)

	if err := fd.db.Create(&FakeDidMapping{
		Handle:  handle,
		Did:     d,
		Service: service,
	}).Error; err != nil {
		return "", err
	}

	return d, nil
}
