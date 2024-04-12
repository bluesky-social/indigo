package search

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ParseQuery takes a query string and pulls out some facet patterns ("from:handle.net") as filters
func ParsePostQuery(ctx context.Context, dir identity.Directory, raw string, viewer *syntax.DID) PostSearchParams {
	quoted := false
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		if r == '"' {
			quoted = !quoted
		}
		return r == ' ' && !quoted
	})

	params := PostSearchParams{}

	keep := make([]string, 0, len(parts))
	for _, p := range parts {
		// pass-through quoted, either phrase or single token
		if strings.HasPrefix(p, "\"") {
			keep = append(keep, p)
			continue
		}

		// tags (array)
		if strings.HasPrefix(p, "#") && len(p) > 1 {
			params.Tags = append(params.Tags, p[1:])
			continue
		}

		// handle (mention)
		if strings.HasPrefix(p, "@") && len(p) > 1 {
			handle, err := syntax.ParseHandle(p[1:])
			if err != nil {
				keep = append(keep, p)
				continue
			}
			id, err := dir.LookupHandle(ctx, handle)
			if err != nil {
				if err != identity.ErrHandleNotFound {
					slog.Error("failed to resolve handle", "err", err)
				}
				continue
			}
			params.Mentions = &id.DID
			continue
		}

		tokParts := strings.SplitN(p, ":", 2)
		if len(tokParts) == 1 {
			keep = append(keep, p)
			continue
		}

		switch tokParts[0] {
		case "did":
			// Used as a hack for `from:me` when suppplied by the client
			did, err := syntax.ParseDID(p)
			if err != nil {
				continue
			}
			params.Author = &did
			continue
		case "from", "to", "mentions":
			raw := tokParts[1]
			if raw == "me" {
				if viewer != nil && tokParts[0] == "from" {
					params.Author = viewer
				} else if viewer != nil {
					params.Mentions = viewer
				}
				continue
			}
			if strings.HasPrefix(raw, "@") && len(raw) > 1 {
				raw = raw[1:]
			}
			handle, err := syntax.ParseHandle(raw)
			if err != nil {
				continue
			}
			id, err := dir.LookupHandle(ctx, handle)
			if err != nil {
				if err != identity.ErrHandleNotFound {
					slog.Error("failed to resolve handle", "err", err)
				}
				continue
			}
			if tokParts[0] == "from" {
				params.Author = &id.DID
			} else {
				params.Mentions = &id.DID
			}
			continue
		case "http", "https":
			params.URL = p
			continue
		case "domain":
			params.Domain = tokParts[1]
			continue
		case "lang":
			lang, err := syntax.ParseLanguage(tokParts[1])
			if nil == err {
				params.Lang = &lang
			}
			continue
		case "since", "until":
			var dt syntax.Datetime
			// first try just date
			date, err := time.Parse(time.DateOnly, tokParts[1])
			if nil == err {
				dt = syntax.Datetime(date.Format(syntax.AtprotoDatetimeLayout))
			} else {
				// fallback to formal atproto datetime format
				dt, err = syntax.ParseDatetimeLenient(tokParts[1])
				if err != nil {
					continue
				}
			}
			if tokParts[0] == "since" {
				params.Since = &dt
			} else {
				params.Until = &dt
			}
			continue
		}

		keep = append(keep, p)
	}

	out := ""
	for _, p := range keep {
		if out == "" {
			out = p
		} else {
			out += " " + p
		}
	}
	if out == "" {
		out = "*"
	}
	params.Query = out
	return params
}
