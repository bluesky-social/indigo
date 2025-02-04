package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/api/agnostic"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CrawlLexicon(ctx context.Context, db *gorm.DB, nsid syntax.NSID, reason string) error {

	// TODO: inject directory
	dir := identity.BaseDirectory{}

	domain, err := extractDomain(nsid)
	if err != nil {
		return fmt.Errorf("extracting domain for NSID: %w", err)
	}
	group, err := extractGroup(nsid)
	if err != nil {
		return fmt.Errorf("extracting group for NSID: %w", err)
	}

	tx := db.WithContext(ctx)
	crawl := &Crawl{
		NSID:   nsid,
		Reason: reason,
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

	// resolve
	did, err := dir.ResolveNSID(ctx, nsid)
	if err != nil {
		crawl.Status = "error-resolve-nsid"
		tx.Create(crawl)
		return err
	}
	crawl.DID = did
	// TODO: normalize DID?
	ident, err := dir.LookupDID(ctx, did)
	if err != nil {
		crawl.Status = "error-did"
		tx.Create(crawl)
		return err
	}
	// TODO: "proof chain"
	xrpcc := &xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	resp, err := agnostic.RepoGetRecord(ctx, xrpcc, "", "com.atproto.lexicon.schema", ident.DID.String(), nsid.String())
	if err != nil {
		crawl.Status = "error-repo-fetch"
		tx.Create(crawl)
		return err
	}
	if nil == resp.Value {
		crawl.Status = "empty-record"
		tx.Create(crawl)
		return fmt.Errorf("empty record in response")
	}
	cid, err := syntax.ParseCID(*resp.Cid)
	if err != nil {
		crawl.Status = "bad-record-cid"
		tx.Create(crawl)
		return err
	}
	crawl.RecordCID = cid

	// verify schema
	var sf lexicon.SchemaFile
	if err := json.Unmarshal(*resp.Value, &sf); err != nil {
		return fmt.Errorf("fetched Lexicon schema record was invalid: %w", err)
	}
	// TODO: check that NSID matches record field
	// TODO: CheckSchema() on lexicon.SchemaFile which handles this
	for _, def := range sf.Defs {
		if err := def.CheckSchema(); err != nil {
			crawl.Status = "bad-schema-check"
			tx.Create(crawl)
			return fmt.Errorf("lexicon format was invalid: %w", err)
		}
	}

	latest, err := comatproto.SyncGetLatestCommit(ctx, xrpcc, did.String())
	if err != nil {
		return err
	}
	crawl.RepoRev = latest.Rev

	if dom.Domain != domain {
		dom = Domain{Domain: domain}
		tx.Create(&dom)
	}

	crawl.Status = "success"
	tx.Create(crawl)

	version := &Version{
		RecordCID: cid,
		NSID:      nsid,
		Record:    *resp.Value,
	}
	tx.Clauses(clause.OnConflict{DoNothing: true}).Create(version)

	lex := &Lexicon{
		NSID:   nsid,
		Domain: domain,
		Group:  group,
		Latest: cid,
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
