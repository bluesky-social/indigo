package plc

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

type FakeDidMapping struct {
	gorm.Model
	Handle      string
	Did         string `gorm:"index"`
	Service     string
	KeyType     string
	PubKeyMbase string
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

		VerificationMethod: []did.VerificationMethod{
			did.VerificationMethod{
				ID:                 "#signingKey",
				Type:               rec.KeyType,
				PublicKeyMultibase: &rec.PubKeyMbase,
				Controller:         rec.Did,
			},
		},

		Service: []did.Service{
			did.Service{
				//ID:              "",
				Type:            "pds",
				ServiceEndpoint: "http://" + rec.Service,
			},
		},
	}, nil
}

func (fd *FakeDid) CreateDID(ctx context.Context, sigkey *did.PrivKey, recovery string, handle string, service string) (string, error) {
	buf := make([]byte, 8)
	rand.Read(buf)
	d := "did:plc:" + hex.EncodeToString(buf)

	if err := fd.db.Create(&FakeDidMapping{
		Handle:      handle,
		Did:         d,
		Service:     service,
		PubKeyMbase: sigkey.Public().MultibaseString(),
		KeyType:     sigkey.KeyType(),
	}).Error; err != nil {
		return "", err
	}

	return d, nil
}

func (fd *FakeDid) UpdateUserHandle(ctx context.Context, did string, nhandle string) error {
	if err := fd.db.Model(FakeDidMapping{}).Where("did = ?", did).UpdateColumn("handle", nhandle).Error; err != nil {
		return err
	}

	return nil
}
