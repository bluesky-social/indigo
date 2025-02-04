package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
)

//go:embed example.lexicon.record.json
var exampleRecordJSON []byte

//go:embed example.lexicon.query.json
var exampleQueryJSON []byte

// e.GET("/demo/record", srv.WebDemoRecord)
func (srv *WebServer) WebDemoRecord(c echo.Context) error {
	info := pongo2.Context{}

	nsid := syntax.NSID("example.lexicon.record")
	lex := Lexicon{
		NSID:   nsid,
		Domain: "lexicon.example",
		Group:  "example.lexicon",
		Latest: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
	}
	ver := Version{
		RecordCID: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
		NSID:      nsid,
		Record:    exampleRecordJSON,
	}
	crawl := Crawl{
		ID: 123,
		// XXX: CreatedAt: "2025-02-04T00:03:07.091Z",
		NSID:      nsid,
		DID:       syntax.DID("did:web:lexicon.example"),
		RecordCID: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
		RepoRev:   "3lhcqvfmmqh22",
		Reason:    "demo",
		//Extra
	}

	var sf lexicon.SchemaFile
	if err := json.Unmarshal(ver.Record, &sf); err != nil {
		return fmt.Errorf("Lexicon schema record was invalid: %w", err)
	}
	defs, err := ParseSchemaFile(&sf, nsid)
	if err != nil {
		return err
	}

	info["lexicon"] = lex
	info["version"] = ver
	info["crawl"] = crawl
	info["defs"] = defs
	return c.Render(http.StatusOK, "lexicon.html", info)
}

// e.GET("/demo/query", srv.WebDemoQuery)
func (srv *WebServer) WebDemoQuery(c echo.Context) error {
	info := pongo2.Context{}

	nsid := syntax.NSID("example.lexicon.query")
	lex := Lexicon{
		NSID:   nsid,
		Domain: "lexicon.example",
		Group:  "example.lexicon",
		Latest: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
	}
	ver := Version{
		RecordCID: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
		NSID:      nsid,
		Record:    exampleQueryJSON,
	}
	crawl := Crawl{
		ID: 123,
		// XXX: CreatedAt: "2025-02-04T00:03:07.091Z",
		NSID:      nsid,
		DID:       syntax.DID("did:web:lexicon.example"),
		RecordCID: "bafyreihbqdb64s3mwyfr2n3pt7dwxxaebbxntz7fchlaafzbtu5ps45i2u",
		RepoRev:   "3lhcqvfmmqh22",
		Reason:    "demo",
		//Extra
	}

	var sf lexicon.SchemaFile
	if err := json.Unmarshal(ver.Record, &sf); err != nil {
		return fmt.Errorf("Lexicon schema record was invalid: %w", err)
	}
	defs, err := ParseSchemaFile(&sf, nsid)
	if err != nil {
		return err
	}

	info["lexicon"] = lex
	info["version"] = ver
	info["crawl"] = crawl
	info["defs"] = defs
	return c.Render(http.StatusOK, "lexicon.html", info)
}
