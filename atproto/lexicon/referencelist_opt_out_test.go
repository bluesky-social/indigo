package lexicon

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReferenceListOptOutLexicon(t *testing.T) {
	cat := NewBaseCatalog()
	require.NoError(t, cat.LoadDirectory("../../lexicons"))

	const ref = "app.bsky.graph.referencelistOptOut"
	valid := map[string]any{
		"$type":     ref,
		"subject":   "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.graph.list/3m2lp5zsc7422",
		"createdAt": "2026-08-25T12:30:00.000Z",
	}
	require.NoError(t, ValidateRecord(cat, valid, ref, 0))

	malformed := map[string]any{
		"$type":     ref,
		"subject":   "not-an-at-uri",
		"createdAt": "2026-08-25T12:30:00.000Z",
	}
	require.Error(t, ValidateRecord(cat, malformed, ref, 0))

	// The collection constraint is enforced by consuming services, not AT URI syntax.
	nonList := map[string]any{
		"$type":     ref,
		"subject":   "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.feed.post/3m2lp5zsc7422",
		"createdAt": "2026-08-25T12:30:00.000Z",
	}
	require.NoError(t, ValidateRecord(cat, nonList, ref, 0))
}

func TestReferenceListOptOutViewState(t *testing.T) {
	cat := NewBaseCatalog()
	require.NoError(t, cat.LoadDirectory("../../lexicons"))

	listItem, err := cat.Resolve("app.bsky.graph.defs#listItemView")
	require.NoError(t, err)
	item := map[string]any{
		"uri": "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.graph.listitem/3m2lp5zsc7422",
		"subject": map[string]any{
			"did":    "did:plc:ewvi7nxzyoun6zhxrhs64oiz",
			"handle": "alice.test",
		},
		"subjectOptedOut": true,
	}
	require.NoError(t, validateData(cat, listItem.Def, item, 0))
	item["subjectOptedOut"] = false
	require.Error(t, validateData(cat, listItem.Def, item, 0))

	viewerState, err := cat.Resolve("app.bsky.graph.defs#listViewerState")
	require.NoError(t, err)
	require.NoError(t, validateData(cat, viewerState.Def, map[string]any{
		"referenceListOptOuts": []any{
			"at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.graph.referencelistOptOut/3m2lp5zsc7422",
		},
	}, 0))
}
