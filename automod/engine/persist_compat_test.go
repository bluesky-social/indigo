package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordModStateRequestCompatibility(t *testing.T) {
	body, err := os.ReadFile("testdata/ozone_getrecord_post.json")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(body, &response))
	labels := response["labels"].([]any)
	labels[0].(map[string]any)["neg"] = true
	labels[1].(map[string]any)["exp"] = "2000-01-02T00:00:00Z"
	body, err = json.Marshal(response)
	require.NoError(t, err)
	type request struct {
		method, path, uri, cid, accept, authorization, proxy string
		hasCID                                               bool
	}
	requests := make(chan request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- request{r.Method, r.URL.Path, r.URL.Query().Get("uri"), r.URL.Query().Get("cid"), r.Header.Get("Accept"), r.Header.Get("Authorization"), r.Header.Get("Atproto-Proxy"), r.URL.Query().Has("cid")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := atclient.NewAdminClient(server.URL, "synthetic-password")
	client.Client = server.Client()
	client.Headers.Set("Accept", "application/vnd.example+json")
	client.Headers.Set("Atproto-Proxy", "did:example:service#atproto_labeler")
	uri := syntax.ATURI(testRecordURI)

	for _, recordCID := range []string{testRecordCID, ""} {
		old, err := toolsozone.ModerationGetRecord(context.Background(), client, recordCID, uri.String())
		require.NoError(t, err)
		current, err := fetchRecordModState(context.Background(), client, uri, syntax.CID(recordCID))
		require.NoError(t, err)
		oldRequest, currentRequest := <-requests, <-requests
		assert.Equal(t, oldRequest, currentRequest)
		assert.Equal(t, http.MethodGet, currentRequest.method)
		assert.Equal(t, "/xrpc/tools.ozone.moderation.getRecord", currentRequest.path)
		assert.Equal(t, uri.String(), currentRequest.uri)
		assert.Equal(t, recordCID, currentRequest.cid)
		assert.Equal(t, recordCID != "", currentRequest.hasCID)
		assert.Equal(t, "application/vnd.example+json", currentRequest.accept)
		assert.NotEmpty(t, currentRequest.authorization)
		assert.Equal(t, "did:example:service#atproto_labeler", currentRequest.proxy)
		assert.Equal(t, old.Labels, current.Labels)
		require.NotNil(t, old.Moderation.SubjectStatus)
		require.NotNil(t, current.Moderation.SubjectStatus)
		assert.Equal(t, old.Moderation.SubjectStatus.Tags, current.Moderation.SubjectStatus.Tags)
		assert.Equal(t, old.Moderation.SubjectStatus.ReviewState, current.Moderation.SubjectStatus.ReviewState)
	}
}

// Source-agnostic deduplication currently treats an author-controlled self-label
// as existing, which can suppress the labeler's own action. This also applies
// to unregistered collections; a change to label-source policy is separate.
func TestPersistRecordDedupesSelfLabels(t *testing.T) {
	for _, collection := range []string{"app.bsky.feed.post", "com.example.thirdparty.photo"} {
		t.Run(collection, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"labels": []any{map[string]any{"src": testSubjectDID, "val": "example"}},
				})
			}
			eng, c, events := persistFixture(t, collection, handler)
			c.AddRecordLabel("example")
			require.NoError(t, eng.persistRecordModActions(c))
			assert.Empty(t, *events)
		})
	}
}

func TestPersistRecordReviewActions(t *testing.T) {
	const prefix = "tools.ozone.moderation.defs#"
	cases := []struct {
		name     string
		status   any
		action   func(*RecordContext)
		wantType string
	}{
		{"escalate missing status", nil, (*RecordContext).EscalateRecord, "modEventEscalate"},
		{"acknowledge missing status", nil, (*RecordContext).AcknowledgeRecord, "modEventAcknowledge"},
		{"escalate open", map[string]any{"reviewState": prefix + "reviewOpen"}, (*RecordContext).EscalateRecord, "modEventEscalate"},
		{"already escalated", map[string]any{"reviewState": prefix + "reviewEscalated"}, (*RecordContext).EscalateRecord, ""},
		{"acknowledge open", map[string]any{"reviewState": prefix + "reviewOpen"}, (*RecordContext).AcknowledgeRecord, "modEventAcknowledge"},
		{"acknowledge closed", map[string]any{"reviewState": prefix + "reviewClosed"}, (*RecordContext).AcknowledgeRecord, ""},
		{"acknowledge none", map[string]any{"reviewState": prefix + "reviewNone"}, (*RecordContext).AcknowledgeRecord, ""},
		{"nil review state", map[string]any{"reviewState": nil}, (*RecordContext).AcknowledgeRecord, "modEventAcknowledge"},
		{"nil review state escalation", map[string]any{"reviewState": nil}, (*RecordContext).EscalateRecord, "modEventEscalate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"moderation": map[string]any{"subjectStatus": tc.status}})
			}
			eng, c, events := persistFixture(t, "com.example.thirdparty.photo", handler)
			tc.action(c)
			require.NoError(t, eng.persistRecordModActions(c))
			if tc.wantType == "" {
				assert.Empty(t, *events)
				return
			}
			require.Len(t, *events, 1)
			assert.Equal(t, prefix+tc.wantType, (*events)[0]["event"].(map[string]any)["$type"])
		})
	}
}

func TestPersistRecordTagsAndLookupFailure(t *testing.T) {
	for _, lookupFails := range []bool{false, true} {
		name := "existing state"
		if lookupFails {
			name = "lookup failure"
		}
		t.Run(name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				if lookupFails {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":"RecordNotFound"}`)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"labels":     []any{map[string]any{"val": "existing"}},
					"moderation": map[string]any{"subjectStatus": map[string]any{"reviewState": "tools.ozone.moderation.defs#reviewOpen", "tags": []string{"existing"}}},
				})
			}
			eng, c, events := persistFixture(t, "com.example.thirdparty.photo", handler)
			c.AddRecordLabel("existing")
			c.AddRecordLabel("new")
			c.AddRecordTag("existing")
			c.AddRecordTag("new")
			require.NoError(t, eng.persistRecordModActions(c))
			require.Len(t, *events, 2)
			label := (*events)[0]["event"].(map[string]any)
			tag := (*events)[1]["event"].(map[string]any)
			want := []any{"new"}
			if lookupFails {
				want = []any{"existing", "new"}
			}
			assert.Equal(t, want, label["createLabelVals"])
			assert.Equal(t, want, tag["add"])
			for _, event := range *events {
				assert.Equal(t, c.RecordOp.ATURI().String(), event["subject"].(map[string]any)["uri"])
				assert.Equal(t, testRecordCID, event["subject"].(map[string]any)["cid"])
			}
		})
	}
}

func TestPersistRecordKeepsTakedownAndFlagWhenLabelIsDeduped(t *testing.T) {
	eng, c, events := persistFixture(t, "com.example.thirdparty.photo", existingLabels("existing"))
	c.AddRecordLabelWithReportAction("existing", testReportAction("example/skipped", testSpamReason))
	c.TakedownRecordWithReportAction(testReportAction("example/takedown", testOtherReason))
	c.TakedownBlob(testRecordCID)
	c.AddRecordFlag("example-flag")

	require.NoError(t, eng.persistRecordModActions(c))
	require.Len(t, *events, 1)
	body := (*events)[0]
	assert.Equal(t, "tools.ozone.moderation.defs#modEventTakedown", body["event"].(map[string]any)["$type"])
	assert.Equal(t, c.RecordOp.ATURI().String(), body["subject"].(map[string]any)["uri"])
	assert.Equal(t, testRecordCID, body["subject"].(map[string]any)["cid"])
	assert.Equal(t, []any{testRecordCID}, body["subjectBlobCids"])
	assert.Equal(t, map[string]any{"types": []any{testOtherReason}, "isAutomated": true}, body["reportAction"])
	flags, err := eng.Flags.Get(c.Ctx, c.RecordOp.ATURI().String())
	require.NoError(t, err)
	assert.Equal(t, []string{"example-flag"}, flags)
}

func TestProcessRecordOpPreservesRecordAndBlobRules(t *testing.T) {
	const blobCID = "bafkreiehogetyj72v5zl4ytlgsla5e2nirhx65w7q6lxhurjqbuldmpuua"
	for _, collection := range []string{"app.bsky.feed.post", "com.example.thirdparty.photo"} {
		t.Run(collection, func(t *testing.T) {
			pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/xrpc/com.atproto.sync.getBlob", r.URL.Path)
				assert.Equal(t, testSubjectDID, r.URL.Query().Get("did"))
				assert.Equal(t, blobCID, r.URL.Query().Get("cid"))
				_, _ = io.WriteString(w, "synthetic image")
			}))
			defer pds.Close()
			eng, c, events := persistFixture(t, collection, existingLabels("existing"))
			dir := identity.NewMockDirectory()
			dir.Insert(identity.Identity{DID: syntax.DID(testSubjectDID), Handle: "subject.example.test", Services: map[string]identity.ServiceEndpoint{"atproto_pds": {URL: pds.URL}}})
			eng.Directory = dir
			eng.Config.SkipAccountMeta = true
			eng.BskyClient = atclient.NewAPIClient("https://appview.example.test")
			eng.BlobClient = pds.Client()
			seenRecord, seenPost := false, false
			blobSeen := make(chan bool, 1)
			eng.Rules = RuleSet{
				RecordRules: []RecordRuleFunc{func(rc *RecordContext) error {
					record, err := atdata.UnmarshalCBOR(rc.RecordOp.RecordCBOR)
					if assert.NoError(t, err) {
						assert.Equal(t, "synthetic record", record["text"])
						seenRecord = true
					}
					rc.AddRecordLabel("existing")
					return nil
				}},
				PostRules: []PostRuleFunc{func(_ *RecordContext, post *appbsky.FeedPost) error {
					assert.Equal(t, "synthetic record", post.Text)
					seenPost = true
					return nil
				}},
				BlobRules: []BlobRuleFunc{func(rc *RecordContext, blob lexutil.LexBlob, data []byte) error {
					assert.Equal(t, blobCID, blob.Ref.String())
					assert.Equal(t, "synthetic image", string(data))
					blobSeen <- true
					rc.AddRecordLabel("new")
					return nil
				}},
			}
			parsedCID, err := cid.Decode(blobCID)
			require.NoError(t, err)
			record, err := atdata.MarshalCBOR(map[string]any{
				"$type": collection, "text": "synthetic record", "createdAt": "2000-01-01T00:00:00Z",
				"image": atdata.Blob{Ref: atdata.CIDLink(parsedCID), MimeType: "image/png", Size: 15},
			})
			require.NoError(t, err)
			op := c.RecordOp
			op.Action, op.RecordCBOR = UpdateOp, record
			require.NoError(t, eng.ProcessRecordOp(context.Background(), op))
			assert.True(t, seenRecord)
			assert.Equal(t, collection == "app.bsky.feed.post", seenPost)
			select {
			case <-blobSeen:
			default:
				t.Fatal("blob rule did not run")
			}
			require.Len(t, *events, 1)
			create, negate := labelEvent(t, (*events)[0])
			assert.Equal(t, []any{"new"}, create)
			assert.Empty(t, negate)
		})
	}
}
