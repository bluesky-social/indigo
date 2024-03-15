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

		// Hashtag Parsing
		if strings.HasPrefix(p, "#") && len(p) > 1 {
			filters = append(filters, map[string]interface{}{
				"term": map[string]interface{}{
					"tag": map[string]interface{}{
						"value":            p[1:],
						"case_insensitive": true,
					},
				},
			})
			continue
		}

		// DID Parsing
		if strings.HasPrefix(p, "did:") {
			filters = append(filters, map[string]interface{}{
				"term": map[string]interface{}{"did": p},
			})
			continue
		}

		// Explicit Handle/DID Parsing
		if strings.HasPrefix(p, "from:") && len(p) > 6 {
			did, err := identifierToDID(ctx, dir, p[5:])
			if nil == err {
				filters = append(filters, map[string]interface{}{
					"term": map[string]interface{}{"from": did.String()},
				})
				continue
			}
			slog.Error("failed to parse identifier in from: clause", "error", err)
			continue
		}

		// Bsky URL Parsing (for quote posts)
		if strings.HasPrefix(p, "https://bsky.app/profile/") {
			atURI, err := bskyURLtoATURI(ctx, dir, p)
			if nil == err {
				filters = append(filters, map[string]interface{}{
					"term": map[string]interface{}{"embed_aturi": atURI.String()},
				})
				continue
			}
			slog.Error("failed to parse bsky URL", "error", err)
		}

		keep = append(keep, p)
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

func identifierToDID(ctx context.Context, dir identity.Directory, term string) (*syntax.DID, error) {
	atID, err := syntax.ParseAtIdentifier(term)
	if err != nil {
		return nil, fmt.Errorf("failed to parse identifier: %w", err)
	}

	if atID.IsDID() {
		asDID, _ := atID.AsDID()
		return &asDID, nil
	}

	if atID.IsHandle() {
		asHandle, _ := atID.AsHandle()
		id, err := dir.LookupHandle(ctx, asHandle)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve handle: %w", err)
		}
		return &id.DID, nil
	}

	return nil, fmt.Errorf("unexpectedly reached bottom of parsing: %s", term)
}

func bskyURLtoATURI(ctx context.Context, dir identity.Directory, term string) (*syntax.ATURI, error) {
	// split up the URL
	parts := strings.Split(term, "/")
	if len(parts) < 7 {
		return nil, fmt.Errorf("Post URL missing parts: %s", term)
	}

	if parts[3] != "profile" && parts[5] != "post" {
		return nil, fmt.Errorf("unexpected URL pattern: not of the form /profile/<ident>/post/<rkey>")
	}

	rkey, err := syntax.ParseRecordKey(parts[6])
	if err != nil {
		return nil, fmt.Errorf("failed to parse record key: %w", err)
	}

	did, err := identifierToDID(ctx, dir, parts[4])
	if err != nil {
		return nil, fmt.Errorf("failed to parse identifier: %w", err)
	}

	atURI, err := syntax.ParseATURI(fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, rkey))
	if err != nil {
		return nil, fmt.Errorf("failed to construct ATURI: %w", err)
	}

	return &atURI, nil

}
