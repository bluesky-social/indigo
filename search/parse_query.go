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

	tags := []string{}

	keep := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "\"")

		if strings.HasPrefix(p, "#") && len(p) > 1 {
			tags = append(tags, p[1:])
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

	if len(tags) > 0 {
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{"tag": tags},
		})
	}

	out := ""
	for _, p := range keep {
		if strings.ContainsRune(p, ' ') {
			if out == "" {
				out = fmt.Sprintf(`"%s"`, p)
			} else {
				out += " " + fmt.Sprintf(`"%s"`, p)
			}
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
