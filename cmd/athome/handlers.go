package main

import (
	"fmt"
	"net/http"

	appbsky "github.com/bluesky-social/indigo/api/bsky"

	"github.com/flosch/pongo2/v6"
	"github.com/labstack/echo/v4"
)

func (srv *Server) WebHome(c echo.Context) error {
	data := pongo2.Context{}
	return c.Render(http.StatusOK, "home.html", data)
}

func (srv *Server) WebPost(c echo.Context) error {
	data := pongo2.Context{}
	handle := c.Param("handle")
	rkey := c.Param("rkey")
	// sanity check argument
	if len(handle) > 4 && len(handle) < 128 && len(rkey) > 0 {
		ctx := c.Request().Context()
		// requires two fetches: first fetch profile (!)
		pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle)
		if err != nil {
			slog.Warn("failed to fetch handle", "handle", handle, "err", err)
			// TODO: only if "not found"
			return echo.NewHTTPError(404, "handle not found: %s", handle)
		} else {
			did := pv.Did
			data["did"] = did

			// then fetch the post thread (with extra context)
			uri := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, rkey)
			// TODO: more of thread?
			tpv, err := appbsky.FeedGetPostThread(ctx, srv.xrpcc, 1, 1, uri)
			if err != nil {
				slog.Warn("failed to fetch post", "uri", uri, "err", err)
				// TODO: only if "not found"
				return echo.NewHTTPError(404, "post not found: %s", handle)
			} else {
				req := c.Request()
				postView := tpv.Thread.FeedDefs_ThreadViewPost.Post
				data["postView"] = postView
				data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
				if postView.Embed != nil && postView.Embed.EmbedImages_View != nil {
					data["imgThumbUrl"] = postView.Embed.EmbedImages_View.Images[0].Thumb
				}
			}
		}

	}
	return c.Render(http.StatusOK, "post.html", data)
}

func (srv *Server) WebProfile(c echo.Context) error {
	data := pongo2.Context{}
	handle := c.Param("handle")
	// sanity check argument
	if len(handle) > 4 && len(handle) < 128 {
		ctx := c.Request().Context()
		pv, err := appbsky.ActorGetProfile(ctx, srv.xrpcc, handle)
		if err != nil {
			slog.Warn("failed to fetch handle", "handle", handle, "err", err)
			// TODO: only if "not found"
			return echo.NewHTTPError(404, "handle not found: %s", handle)
		} else {
			req := c.Request()
			data["profileView"] = pv
			data["requestURI"] = fmt.Sprintf("https://%s%s", req.Host, req.URL.Path)
		}
		af, err := appbsky.FeedGetAuthorFeed(ctx, srv.xrpcc, handle, "", "", 100)
		if err != nil {
			slog.Warn("failed to fetch author feed", "handle", handle, "err", err)
			// TODO: show some error?
		} else {
			data["authorFeed"] = af.Feed
			//slog.Warn("author feed", "feed", af.Feed)
		}
	}

	return c.Render(http.StatusOK, "profile.html", data)
}
