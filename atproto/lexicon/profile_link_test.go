package lexicon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"

	"github.com/stretchr/testify/require"
)

// plData round-trips through JSON so that blob objects become atdata.Blob, as in real records.
func plData(t *testing.T, m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	require.NoError(t, err)
	d, err := atdata.UnmarshalJSON(b)
	require.NoError(t, err)
	return d
}

const (
	plDID     = "did:plc:ewvi7nxzyoun6zhxrhs64oiz"
	plCID     = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"
	plBlobCID = "bafkreibme22gw2h7y2h7tg2fhqotaqjucnbc24deqo72b6mkl2egezxhvy"
	plLinkURI = "at://" + plDID + "/app.bsky.actor.link/3m2lp5zsc7422"
)

func plLink(mod func(map[string]any)) map[string]any {
	m := map[string]any{
		"$type":     "app.bsky.actor.link",
		"url":       "https://example.com/",
		"createdAt": "2026-08-25T12:30:00.000Z",
	}
	if mod != nil {
		mod(m)
	}
	return m
}

func TestProfileLinkRecord(t *testing.T) {
	cat := NewBaseCatalog()
	require.NoError(t, cat.LoadDirectory("../../lexicons"))
	const ref = "app.bsky.actor.link"

	require.NoError(t, ValidateRecord(cat, plData(t, plLink(nil)), ref, 0))

	require.NoError(t, ValidateRecord(cat, plData(t, plLink(func(m map[string]any) {
		m["title"] = "Example"
		m["icon"] = map[string]any{
			"$type":    "blob",
			"ref":      map[string]any{"$link": plBlobCID},
			"mimeType": "image/png",
			"size":     1234,
		}
		m["labels"] = map[string]any{
			"$type":  "com.atproto.label.defs#selfLabels",
			"values": []any{map[string]any{"val": "porn"}},
		}
	})), ref, 0))

	for name, mod := range map[string]func(map[string]any){
		"missing url":       func(m map[string]any) { delete(m, "url") },
		"missing createdAt": func(m map[string]any) { delete(m, "createdAt") },
		"invalid url":       func(m map[string]any) { m["url"] = "not a uri" },
		"invalid createdAt": func(m map[string]any) { m["createdAt"] = "yesterday" },
		"overlong title":    func(m map[string]any) { m["title"] = strings.Repeat("a", 41) },
		"wrong icon type": func(m map[string]any) {
			m["icon"] = map[string]any{
				"$type":    "blob",
				"ref":      map[string]any{"$link": plBlobCID},
				"mimeType": "image/gif",
				"size":     1234,
			}
		},
		"oversize icon": func(m map[string]any) {
			m["icon"] = map[string]any{
				"$type":    "blob",
				"ref":      map[string]any{"$link": plBlobCID},
				"mimeType": "image/png",
				"size":     100001,
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ValidateRecord(cat, plData(t, plLink(mod)), ref, 0))
		})
	}
}

func TestProfileLinksField(t *testing.T) {
	cat := NewBaseCatalog()
	require.NoError(t, cat.LoadDirectory("../../lexicons"))
	const ref = "app.bsky.actor.profile"

	profile := func(n int) map[string]any {
		links := []any{}
		for i := 0; i < n; i++ {
			links = append(links, map[string]any{
				"uri": fmt.Sprintf("at://%s/app.bsky.actor.link/3m2lp5zsc74%02d", plDID, i),
				"cid": plCID,
			})
		}
		return map[string]any{"$type": ref, "links": links}
	}
	require.NoError(t, ValidateRecord(cat, map[string]any{"$type": ref}, ref, 0))
	require.NoError(t, ValidateRecord(cat, profile(10), ref, 0))
	require.Error(t, ValidateRecord(cat, profile(11), ref, 0))
}

func TestProfileLinkView(t *testing.T) {
	cat := NewBaseCatalog()
	require.NoError(t, cat.LoadDirectory("../../lexicons"))

	view, err := cat.Resolve("app.bsky.actor.defs#profileLinkView")
	require.NoError(t, err)

	data := map[string]any{
		"uri":   plLinkURI,
		"cid":   plCID,
		"url":   "https://example.com/",
		"title": "Example",
		"icon":  "https://cdn.example.com/icon.png",
		"labels": []any{map[string]any{
			"src": plDID,
			"uri": plLinkURI,
			"val": "porn",
			"cts": "2026-08-25T12:30:00.000Z",
		}},
	}
	require.NoError(t, validateData(cat, view.Def, data, 0))

	require.NoError(t, validateData(cat, view.Def, map[string]any{
		"uri": plLinkURI, "cid": plCID, "url": "https://example.com/",
	}, 0))

	for _, missing := range []string{"uri", "cid", "url"} {
		bad := map[string]any{"uri": plLinkURI, "cid": plCID, "url": "https://example.com/"}
		delete(bad, missing)
		require.Error(t, validateData(cat, view.Def, bad, 0), missing)
	}
}
