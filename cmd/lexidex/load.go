package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func loadSchema(ctx context.Context, db *gorm.DB, raw json.RawMessage) error {

	// verify schema
	var sf lexicon.SchemaFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return fmt.Errorf("fetched Lexicon schema record was invalid: %w", err)
	}
	// TODO: CheckSchema needs to be called in a different order... after setting base?
	/*
		for _, def := range sf.Defs {
			if err := def.CheckSchema(); err != nil {
				return fmt.Errorf("lexicon format was invalid: %w", err)
			}
		}
	*/

	nsid, err := syntax.ParseNSID(sf.ID)
	if err != nil {
		return err
	}
	domain, err := extractDomain(nsid)
	if err != nil {
		return fmt.Errorf("extracting domain for NSID: %w", err)
	}
	group, err := extractGroup(nsid)
	if err != nil {
		return fmt.Errorf("extracting group for NSID: %w", err)
	}

	// compute CID
	rec, err := data.UnmarshalJSON(raw)
	if err != nil {
		return err
	}
	cbytes, err := data.MarshalCBOR(rec)
	if err != nil {
		return err
	}
	c, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(cbytes)
	if err != nil {
		return err
	}
	recordCID := syntax.CID(c.String())

	tx := db.WithContext(ctx)
	crawl := &Crawl{
		NSID:      nsid,
		Reason:    "load-file",
		RecordCID: recordCID,
	}

	// check that domain isn't blocked
	var dom Domain
	err = tx.Limit(1).Find(&dom, "domain = ?", domain).Error
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		// pass
	} else if err != nil {
		return err
	}
	if dom.Disabled {
		return fmt.Errorf("not crawling disabled domain: %s", nsid)
	}

	if dom.Domain != domain {
		dom = Domain{Domain: domain}
		tx.Create(&dom)
	}

	crawl.Status = "success"
	tx.Create(crawl)

	version := &Version{
		RecordCID: recordCID,
		NSID:      nsid,
		Record:    raw,
	}
	tx.Clauses(clause.OnConflict{DoNothing: true}).Create(version)

	lex := &Lexicon{
		NSID:   nsid,
		Domain: domain,
		Group:  group,
		Latest: recordCID,
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "nsid"}},
		DoUpdates: clause.AssignmentColumns([]string{"latest"}),
	}).Create(lex)
	if res.Error != nil {
		return fmt.Errorf("error saving crawl to database: %w", res.Error)
	}
	return nil
}

func LoadDirectory(ctx context.Context, db *gorm.DB, p string) error {

	walkFunc := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".json") {
			return nil
		}
		slog.Info("loading Lexicon schema file", "path", p)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		var obj json.RawMessage
		if err := json.Unmarshal(b, &obj); err != nil {
			return fmt.Errorf("Lexicon schema record was invalid: %w", err)
		}
		return loadSchema(ctx, db, obj)
	}
	return filepath.WalkDir(p, walkFunc)
}
