package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEditedRecordCID = "bafyreidqcr4i7ar7e6un54w6qg3sbyv4gqqlg3mspm3gz7x7vrb2xqkwti"

func TestPersistRecordReportDeduplicationUsesCID(t *testing.T) {
	var queries int
	var reportedCIDs []string
	eng, c := reportActionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/tools.ozone.moderation.queryEvents":
			queries++
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
		case "/xrpc/tools.ozone.moderation.emitEvent":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			reportedCIDs = append(reportedCIDs, body["subject"].(map[string]any)["cid"].(string))
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	persist := func(cidString string) {
		t.Helper()
		cid := syntax.CID(cidString)
		c.RecordOp.CID = &cid
		c.effects = &Effects{}
		c.ReportRecord(testOtherReason, "test report")
		require.NoError(t, eng.persistRecordModActions(c))
	}

	persist(testRecordCID)
	persist(testRecordCID)
	persist(testEditedRecordCID)

	assert.Equal(t, 2, queries)
	assert.Equal(t, []string{testRecordCID, testEditedRecordCID}, reportedCIDs)
}

func TestCreateRecordReportIfFreshUsesCID(t *testing.T) {
	existingCID := testRecordCID
	var reportedCIDs []string
	eng, c := reportActionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/tools.ozone.moderation.queryEvents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"events": []any{map[string]any{
					"id":        1,
					"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
					"createdBy": "did:example:moderator",
					"event": map[string]any{
						"$type":      "tools.ozone.moderation.defs#modEventReport",
						"reportType": testOtherReason,
					},
					"subject": map[string]any{
						"$type": "com.atproto.repo.strongRef",
						"uri":   testRecordURI,
						"cid":   existingCID,
					},
				}},
			})
		case "/xrpc/tools.ozone.moderation.emitEvent":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			reportedCIDs = append(reportedCIDs, body["subject"].(map[string]any)["cid"].(string))
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	candidateCID := syntax.CID(testEditedRecordCID)
	report := ModReport{ReasonType: testOtherReason, Comment: "test report"}

	created, err := eng.createRecordReportIfFresh(c.Ctx, eng.OzoneClient, c.RecordOp.ATURI(), &candidateCID, report)
	require.NoError(t, err)
	assert.True(t, created)

	existingCID = testEditedRecordCID
	created, err = eng.createRecordReportIfFresh(c.Ctx, eng.OzoneClient, c.RecordOp.ATURI(), &candidateCID, report)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, []string{testEditedRecordCID}, reportedCIDs)
}
