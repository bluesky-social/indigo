package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (srv *WebServer) WebHome(c echo.Context) error {
	ctx := c.Request().Context()
	info := pongo2.Context{}

	tx := srv.db.WithContext(ctx)
	var domains []Domain
	if err := tx.Where("hidden IS false AND disabled IS false").Find(&domains).Error; err != nil {
		return err
	}

	info["domains"] = domains
	return c.Render(http.StatusOK, "home.html", info)
}

// e.GET("/query", srv.WebQuery)
func (srv *WebServer) WebQuery(c echo.Context) error {

	// parse the q query param, redirect based on that
	q := c.QueryParam("q")
	if q == "" {
		return c.Redirect(http.StatusFound, "/")
	}

	nsid, err := syntax.ParseNSID(q)
	if nil == err {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/lexicon/%s", nsid))
	}
	return echo.NewHTTPError(400, "failed to parse query")
}

// e.GET("/domain/:domain", srv.WebDomain)
func (srv *WebServer) WebDomain(c echo.Context) error {
	ctx := c.Request().Context()
	info := pongo2.Context{}

	domain := c.Param("domain")
	_, err := syntax.ParseHandle(c.Param("domain"))
	if err != nil {
		return echo.NewHTTPError(400, "not a valid domain name")
	}

	tx := srv.db.WithContext(ctx)
	var lexicons []Lexicon
	if err := tx.Where("domain = ?", domain).Find(&lexicons).Error; err != nil {
		return err
	}

	info["domain"] = domain
	info["lexicons"] = lexicons
	return c.Render(http.StatusOK, "domain.html", info)
}

// e.GET("/lexicon/:nsid", srv.WebLexicon)
func (srv *WebServer) WebLexicon(c echo.Context) error {
	ctx := c.Request().Context()
	info := pongo2.Context{}

	nsid, err := syntax.ParseNSID(c.Param("nsid"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse lexicon NSID")
	}

	tx := srv.db.WithContext(ctx)
	var lex Lexicon
	if err := tx.First(&lex, "nsid = ?", nsid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(404, "lexicon not known")
		} else {
			return err
		}
	}
	var ver Version
	if err := tx.First(&ver, "record_cid = ?", lex.Latest).Error; err != nil {
		return err
	}
	var crawl Crawl
	if err := tx.Last(&crawl, "record_cid = ?", lex.Latest).Error; err != nil {
		return err
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
	info["uri"] = nsid
	return c.Render(http.StatusOK, "lexicon.html", info)
}

func (srv *WebServer) WebLexiconHistory(c echo.Context) error {
	ctx := c.Request().Context()
	info := pongo2.Context{}

	nsid, err := syntax.ParseNSID(c.Param("nsid"))
	if err != nil {
		return echo.NewHTTPError(400, "failed to parse lexicon NSID")
	}

	tx := srv.db.WithContext(ctx)
	var lex Lexicon
	if err := tx.First(&lex, "nsid = ?", nsid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(404, "lexicon not known")
		} else {
			return err
		}
	}

	var history []Crawl
	if err := tx.Where("nsid = ?", nsid).Find(&history).Error; err != nil {
		return err
	}

	info["lexicon"] = lex
	info["history"] = history
	info["uri"] = nsid
	return c.Render(http.StatusOK, "history.html", info)
}

func (srv *WebServer) WebRecent(c echo.Context) error {
	ctx := c.Request().Context()
	info := pongo2.Context{}

	tx := srv.db.WithContext(ctx)
	var history []Crawl
	if err := tx.Order("created_at desc").Limit(20).Find(&history).Error; err != nil {
		return err
	}

	info["history"] = history
	return c.Render(http.StatusOK, "recent.html", info)
}
