package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ParseQuery takes a query string and pulls out some facet patterns ("from:handle.net") as filters
func ParseQuery(ctx context.Context, dir identity.Directory, raw string) (string, []map[string]interface{}) {
	var filters []map[string]interface{}
	quoted := false
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		if r == '"' {
			quoted = !quoted
		}
		return r == ' ' && !quoted
	})

	keep := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "\"")

		if !strings.ContainsRune(p, ':') || strings.ContainsRune(p, ' ') {
			// simple: quoted (whitespace), or just a token
			keep = append(keep, p)
			continue
		}
		if strings.HasPrefix(p, "did:") {
			filters = append(filters, map[string]interface{}{
				"term": map[string]interface{}{"did": p},
			})
			continue
		}
		if strings.HasPrefix(p, "from:") && len(p) > 6 {
			handle, err := syntax.ParseHandle(p[5:])
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
			filters = append(filters, map[string]interface{}{
				"term": map[string]interface{}{"did": id.DID.String()},
			})
			continue
		}
		keep = append(keep, p)
	}

	out := ""
	for _, p := range keep {
		if strings.ContainsRune(p, ' ') {
			out += fmt.Sprintf(" \"%s\"", p)
		} else {
			if out == "" {
				out = p
			} else {
				out += " " + p
			}
		}
	}
	if out == "" && len(filters) >= 1 {
		out = "*"
	}
	return out, filters
}
