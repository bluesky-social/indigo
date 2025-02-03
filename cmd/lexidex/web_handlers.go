package main

import (
	"errors"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (srv *WebServer) WebHome(c echo.Context) error {
	info := pongo2.Context{}
	return c.Render(http.StatusOK, "home.html", info)
}

// e.GET("/lexicon/:nsid", srv.WebLexicon)
func (srv *WebServer) WebLexicon(c echo.Context) error {
	ctx := c.Request().Context()
	//req := c.Request()
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

	d, err := data.UnmarshalJSON(ver.Record)
	if err != nil {
		return err
	}

	info["lexicon"] = lex
	info["version"] = ver
	info["crawl"] = crawl
	info["schema"] = d
	return c.Render(http.StatusOK, "lexicon.html", info)
}
