package main

import (
	"fmt"
	"net/http"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
)

func (srv *Server) reqHandle(c echo.Context) syntax.Handle {
	host := c.Request().Host
	host = strings.SplitN(host, ":", 2)[0]
	handle, err := syntax.ParseHandle(host)
	if err != nil {
		slog.Warn("host is not a valid handle, fallback to default", "hostname", host)
		handle = srv.defaultHandle
	}
	return handle
}

func (srv *Server) WebHome(c echo.Context) error {
	return c.Redirect(http.StatusFound, "/bsky")
}

func (srv *Server) WebRepoCar(c echo.Context) error {
	handle := srv.reqHandle(c)
	ident, err := srv.dir.LookupHandle(c.Request().Context(), handle)
	if err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, ident.PDSEndpoint()+"/xrpc/com.atproto.sync.getRepo?did="+ident.DID.String())
}

func (srv *Server) WebPost(c echo.Context) error {
	ctx := c.Request().Context()
	req := c.Request()
	data := pongo2.Context{}
	handle := srv.reqHandle(c)
	// TODO: parse rkey
	rkey := c.Param("rkey")

	// requires two fetches: first fetch profile (!)
	pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle.String())
	if err != nil {
		slog.Warn("failed to fetch handle", "handle", handle, "err", err)
		// TODO: only if "not found"
		return echo.NewHTTPError(404, fmt.Sprintf("handle not found: %s", handle))
	}
	did := pv.Did
	data["did"] = did

	// then fetch the post thread (with extra context)
	aturi := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, rkey)
	tpv, err := appbsky.FeedGetPostThread(ctx, srv.xrpcc, 8, 8, aturi)
	if err != nil {
		slog.Warn("failed to fetch post", "aturi", aturi, "err", err)
		// TODO: only if "not found"
		return echo.NewHTTPError(404, "post not found: %s", handle)
	}
	data["postView"] = tpv.Thread.FeedDefs_ThreadViewPost
	data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
	return c.Render(http.StatusOK, "post.html", data)
}

func (srv *Server) WebProfile(c echo.Context) error {
	ctx := c.Request().Context()
	data := pongo2.Context{}
	handle := srv.reqHandle(c)

	pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle.String())
	if err != nil {
		slog.Warn("failed to fetch handle", "handle", handle, "err", err)
		// TODO: only if "not found"
		return echo.NewHTTPError(404, fmt.Sprintf("handle not found: %s", handle))
	} else {
		req := c.Request()
		data["profileView"] = pv
		data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
	}
	did := pv.Did
	data["did"] = did

	af, err := appbsky.FeedGetAuthorFeed(ctx, srv.xrpcc, handle.String(), "", "posts_no_replies", false, 100)
	if err != nil {
		slog.Warn("failed to fetch author feed", "handle", handle, "err", err)
		// TODO: show some error?
	} else {
		data["authorFeed"] = af.Feed
		//slog.Warn("author feed", "feed", af.Feed)
	}

	return c.Render(http.StatusOK, "profile.html", data)
}

// https://medium.com/@etiennerouzeaud/a-rss-feed-valid-in-go-edfc22e410c7
type Item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

type rss struct {
	Version     string `xml:"version,attr"`
	Description string `xml:"channel>description"`
	Link        string `xml:"channel>link"`
	Title       string `xml:"channel>title"`

	Item []Item `xml:"channel>item"`
}

func (srv *Server) WebRepoRSS(c echo.Context) error {
	ctx := c.Request().Context()
	handle := srv.reqHandle(c)

	pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle.String())
	if err != nil {
		slog.Warn("failed to fetch handle", "handle", handle, "err", err)
		// TODO: only if "not found"
		return echo.NewHTTPError(404, fmt.Sprintf("handle not found: %s", handle))
		//return err
	}

	af, err := appbsky.FeedGetAuthorFeed(ctx, srv.xrpcc, handle.String(), "", "posts_no_replies", false, 30)
	if err != nil {
		slog.Warn("failed to fetch author feed", "handle", handle, "err", err)
		return err
	}

	posts := []Item{}
	for _, p := range af.Feed {
		// only include own posts in RSS
		if p.Post.Author.Did != pv.Did {
			continue
		}
		aturi, err := syntax.ParseATURI(p.Post.Uri)
		if err != nil {
			return err
		}
		rec := p.Post.Record.Val.(*appbsky.FeedPost)
		// only top-level posts in RSS
		if rec.Reply != nil {
			continue
		}
		posts = append(posts, Item{
			Title:       "@" + handle.String() + " post",
			Link:        fmt.Sprintf("https://%s/bsky/post/%s", handle, aturi.RecordKey().String()),
			Description: rec.Text,
			PubDate:     rec.CreatedAt,
		})
	}

	title := "@" + handle.String()
	if pv.DisplayName != nil {
		title = title + " - " + *pv.DisplayName
	}
	desc := ""
	if pv.Description != nil {
		desc = *pv.Description
	}
	feed := &rss{
		Version:     "2.0",
		Description: desc,
		Link:        fmt.Sprintf("https://%s/bsky", handle.String()),
		Title:       title,
		Item:        posts,
	}
	return c.XML(http.StatusOK, feed)
}
