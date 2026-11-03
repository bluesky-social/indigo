package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures are synthetic tools.ozone.moderation.getRecord responses.
var getRecordFixtures = []struct {
	file       string
	collection string
	labels     []string
}{
	{"testdata/ozone_getrecord_thirdparty.json", "com.example.thirdparty.photo", []string{"porn", "porn"}},
	{"testdata/ozone_getrecord_site_standard.json", "site.standard.document", []string{"porn"}},
	{"testdata/ozone_getrecord_post.json", "app.bsky.feed.post", []string{"porn", "porn", "porn", "graphic-media"}},
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func TestFetchRecordModStateDecodesAnyCollection(t *testing.T) {
	for _, fx := range getRecordFixtures {
		t.Run(fx.collection, func(t *testing.T) {
			server := httptest.NewServer(serveFile(t, fx.file))
			defer server.Close()
			client := atclient.NewAPIClient(server.URL)
			uri := syntax.ATURI("at://did:example:subject/" + fx.collection + "/example")

			rv, err := fetchRecordModState(context.Background(), client, uri, syntax.CID(testRecordCID))
			require.NoError(t, err)
			var vals []string
			for _, lbl := range rv.Labels {
				vals = append(vals, lbl.Val)
			}
			assert.Equal(t, fx.labels, vals)
			require.NotNil(t, rv.Moderation)
			require.NotNil(t, rv.Moderation.SubjectStatus)
			assert.NotNil(t, rv.Moderation.SubjectStatus.ReviewState)
		})
	}
}

// Documents why fetchRecordModState exists: the generated response type rejects record types it doesn't know.
func TestGeneratedGetRecordRejectsUnknownCollection(t *testing.T) {
	server := httptest.NewServer(serveFile(t, "testdata/ozone_getrecord_thirdparty.json"))
	defer server.Close()
	client := atclient.NewAPIClient(server.URL)

	_, err := toolsozone.ModerationGetRecord(context.Background(), client, testRecordCID, "at://did:example:subject/com.example.thirdparty.photo/example")
	assert.ErrorContains(t, err, "unrecognized lexicon type")
}

func TestFetchRecordModStateIgnoresUnknownRelatedRecord(t *testing.T) {
	body, err := os.ReadFile("testdata/ozone_getrecord_post.json")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(body, &response))
	repo := response["repo"].(map[string]any)
	repo["relatedRecords"] = []any{map[string]any{"$type": "com.example.unknown.profile"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client := atclient.NewAPIClient(server.URL)
	uri := syntax.ATURI("at://did:example:subject/app.bsky.feed.post/example")

	_, err = toolsozone.ModerationGetRecord(context.Background(), client, testRecordCID, uri.String())
	assert.ErrorContains(t, err, "unrecognized lexicon type")
	state, err := fetchRecordModState(context.Background(), client, uri, syntax.CID(testRecordCID))
	require.NoError(t, err)
	assert.Len(t, state.Labels, 4)
}

// persistFixture returns a record context for the given collection, and records every emitEvent body.
func persistFixture(t *testing.T, collection string, getRecord http.HandlerFunc) (*Engine, *RecordContext, *[]map[string]any) {
	t.Helper()
	events := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/tools.ozone.moderation.getRecord":
			getRecord(w, r)
		case "/xrpc/tools.ozone.moderation.emitEvent":
			var body map[string]any
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				events = append(events, body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	eng := EngineTestFixture()
	eng.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	eng.OzoneClient = atclient.NewAPIClient(server.URL)
	eng.OzoneClient.Client = server.Client()
	moderator := syntax.DID("did:example:moderator")
	eng.OzoneClient.AccountDID = &moderator
	cid := syntax.CID(testRecordCID)
	c := &RecordContext{
		AccountContext: AccountContext{
			BaseContext: BaseContext{Ctx: context.Background(), Logger: eng.Logger, engine: &eng, effects: &Effects{}},
			Account:     AccountMeta{Identity: &identity.Identity{DID: syntax.DID(testSubjectDID)}},
		},
		RecordOp: RecordOp{DID: syntax.DID(testSubjectDID), Collection: syntax.NSID(collection), RecordKey: "example", CID: &cid},
	}
	return &eng, c, &events
}

func existingLabels(vals ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		labels := make([]map[string]any, 0, len(vals))
		for _, val := range vals {
			labels = append(labels, map[string]any{"src": "did:example:moderation", "uri": r.URL.Query().Get("uri"), "val": val, "cts": "2000-01-01T00:00:00Z"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels})
	}
}

func labelEvent(t *testing.T, body map[string]any) (create, negate []any) {
	t.Helper()
	event, ok := body["event"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tools.ozone.moderation.defs#modEventLabel", event["$type"])
	create, _ = event["createLabelVals"].([]any)
	negate, _ = event["negateLabelVals"].([]any)
	return create, negate
}

// A third-party record that already carries the label must not be labelled again when it is rewritten.
func TestPersistRecordDedupesLabelOnUnknownCollection(t *testing.T) {
	eng, c, events := persistFixture(t, "com.example.thirdparty.photo", serveFile(t, "testdata/ozone_getrecord_thirdparty.json"))
	c.AddRecordLabel("porn")

	require.NoError(t, eng.persistRecordModActions(c))
	assert.Empty(t, *events)
}

// A new label on a third-party record is still applied.
func TestPersistRecordAppliesNewLabelOnUnknownCollection(t *testing.T) {
	eng, c, events := persistFixture(t, "com.example.thirdparty.photo", serveFile(t, "testdata/ozone_getrecord_thirdparty.json"))
	c.AddRecordLabel("nudity")

	require.NoError(t, eng.persistRecordModActions(c))
	require.Len(t, *events, 1)
	create, negate := labelEvent(t, (*events)[0])
	assert.Equal(t, []any{"nudity"}, create)
	assert.Empty(t, negate)
}

// A rule can request that a label be removed and re-added. An already present
// label must not be removed just because its addition was de-duplicated.
func TestPersistRecordKeepsReconfirmedLabel(t *testing.T) {
	eng, c, events := persistFixture(t, "app.bsky.actor.profile", existingLabels("porn"))
	for _, val := range []string{"sexual", "sexual-figurative", "porn", "nudity"} {
		c.RemoveRecordLabel(val)
	}
	c.AddRecordLabel("porn")

	require.NoError(t, eng.persistRecordModActions(c))
	assert.Empty(t, *events)
}

// A label that is no longer requested is still removed when another label is applied.
func TestPersistRecordReplacesLabel(t *testing.T) {
	eng, c, events := persistFixture(t, "app.bsky.actor.profile", existingLabels("porn"))
	for _, val := range []string{"sexual", "sexual-figurative", "porn", "nudity"} {
		c.RemoveRecordLabel(val)
	}
	c.AddRecordLabel("nudity")

	require.NoError(t, eng.persistRecordModActions(c))
	require.Len(t, *events, 1)
	create, negate := labelEvent(t, (*events)[0])
	assert.Equal(t, []any{"nudity"}, create)
	assert.Equal(t, []any{"porn"}, negate)
}
