package main

import (
	"encoding/json"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"gorm.io/gorm"
)

func RunAllMigrations(db *gorm.DB) {
	db.AutoMigrate(&Domain{})
	db.AutoMigrate(&Lexicon{})
	db.AutoMigrate(&Version{})
	db.AutoMigrate(&Crawl{})
}

// A domain name with NSIDs grouped below it. Roughly aligns with public-suffix-list, though we might end up with atmosphere-specific overrides. Can hide indexing from the front page, or entirely disable indexing.
type Domain struct {
	Domain   string `gorm:"primaryKey"`
	Hidden   bool
	Disabled bool
}

type Lexicon struct {
	NSID   syntax.NSID `gorm:"primaryKey;column:nsid"`
	Domain string
	Group  string
	Latest syntax.CID
}

type Version struct {
	RecordCID syntax.CID      `gorm:"primaryKey;column:record_cid"`
	NSID      syntax.NSID     `gorm:"index;column:nsid"`
	Record    json.RawMessage `gorm:"serializer:json"`
}

type Crawl struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	NSID      syntax.NSID `gorm:"index;column:nsid"`
	DID       syntax.DID  `gorm:"index;column:did"`
	RecordCID syntax.CID  `gorm:"index;column:record_cid"`
	RepoRev   string
	Reason    string
	Extra     map[string]any `gorm:"serializer:json"`
}
