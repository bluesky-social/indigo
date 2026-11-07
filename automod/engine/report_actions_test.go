package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSpamReason  = "com.atproto.moderation.defs#reasonSpam"
	testOtherReason = "com.atproto.moderation.defs#reasonOther"
	testSubjectDID  = "did:example:subject"
	testRecordCID   = "bafyreiclbzvob7lwc2nfkiszszubsfgplkfxrv7umrhiodt62xclsljkmq"
	testRecordURI   = "at://did:example:subject/app.bsky.feed.post/example"
)

func testReportAction(name string, types ...string) ReportAction {
	return ReportAction{Rule: RuleMetadata{Name: name, Version: 1}, Types: types}
}

// All requests use the generated API client so assertions cover the wire format.
func reportActionFixture(t *testing.T, handler http.HandlerFunc) (*Engine, *RecordContext) {
	t.Helper()
	server := httptest.NewServer(handler)
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
		RecordOp: RecordOp{DID: syntax.DID(testSubjectDID), Collection: "app.bsky.feed.post", RecordKey: "example", CID: &cid},
	}
	return &eng, c
}

func captureModEvents(t *testing.T, existingLabels []string, events *[]map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/tools.ozone.moderation.getRecord":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, testRecordURI, r.URL.Query().Get("uri"))
			assert.Equal(t, testRecordCID, r.URL.Query().Get("cid"))
			labels := make([]map[string]any, 0, len(existingLabels))
			for _, label := range existingLabels {
				labels = append(labels, map[string]any{"val": label})
			}
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"labels": labels}))
		case "/xrpc/tools.ozone.moderation.emitEvent":
			assert.Equal(t, http.MethodPost, r.Method)
			var body map[string]any
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				*events = append(*events, body)
			}
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func assertTargetedEvent(t *testing.T, body map[string]any, record bool, eventType string, types []string, rules []RuleMetadata) {
	t.Helper()
	assert.Equal(t, "did:example:moderator", body["createdBy"])
	subject := map[string]any{"$type": "com.atproto.admin.defs#repoRef", "did": testSubjectDID}
	if record {
		subject = map[string]any{"$type": "com.atproto.repo.strongRef", "uri": testRecordURI, "cid": testRecordCID}
	}
	assert.Equal(t, subject, body["subject"])
	require.IsType(t, map[string]any{}, body["event"])
	assert.Equal(t, "tools.ozone.moderation.defs#"+eventType, body["event"].(map[string]any)["$type"])
	// Equality of the complete selector excludes IDs, broad matching and owned scope.
	rawTypes := make([]any, 0, len(types))
	for _, reason := range types {
		rawTypes = append(rawTypes, reason)
	}
	assert.Equal(t, map[string]any{"types": rawTypes, "isAutomated": true}, body["reportAction"])
	rawRules := make([]any, 0, len(rules))
	for _, rule := range rules {
		rawRules = append(rawRules, map[string]any{"name": rule.Name, "version": float64(rule.Version)})
	}
	assert.Equal(t, map[string]any{"name": "automod", "meta": map[string]any{"source": "automod", "rules": rawRules}}, body["modTool"])
}

func TestReportActionWireScope(t *testing.T) {
	for _, targeted := range []bool{false, true} {
		name := "legacy"
		if targeted {
			name = "targeted"
		}
		t.Run(name, func(t *testing.T) {
			var events []map[string]any
			eng, c := reportActionFixture(t, captureModEvents(t, nil, &events))
			actions := []ReportAction{
				testReportAction("example/account-label", testSpamReason),
				testReportAction("example/account-takedown", testOtherReason),
				testReportAction("example/record-label", testOtherReason),
				testReportAction("example/record-takedown", testSpamReason),
			}
			if targeted {
				c.AddAccountLabelWithReportAction("account-label", actions[0])
				c.TakedownAccountWithReportAction(actions[1])
				c.AddRecordLabelWithReportAction("record-label", actions[2])
				c.TakedownRecordWithReportAction(actions[3])
			} else {
				c.AddAccountLabel("account-label")
				c.TakedownAccount()
				c.AddRecordLabel("record-label")
				c.TakedownRecord()
			}
			require.NoError(t, eng.persistRecordModActions(c))
			require.Len(t, events, 4)
			for i, body := range events {
				eventType := "modEventLabel"
				if i%2 == 1 {
					eventType = "modEventTakedown"
				}
				if targeted {
					assertTargetedEvent(t, body, i >= 2, eventType, actions[i].Types, []RuleMetadata{actions[i].Rule})
				} else {
					assert.NotContains(t, body, "reportAction")
					assert.NotContains(t, body, "modTool")
				}
			}
		})
	}
}

func TestReportActionLabelDeduplication(t *testing.T) {
	for _, record := range []bool{false, true} {
		name := "account"
		if record {
			name = "record"
		}
		t.Run(name, func(t *testing.T) {
			var events []map[string]any
			eng, c := reportActionFixture(t, captureModEvents(t, []string{"existing"}, &events))
			c.Account.AccountLabels = []string{"existing"}
			add := c.AddAccountLabelWithReportAction
			if record {
				add = c.AddRecordLabelWithReportAction
			}
			add("existing", testReportAction("example/skipped", "com.atproto.moderation.defs#reasonViolation"))
			first := testReportAction("example/first", testSpamReason, testOtherReason, testSpamReason)
			second := testReportAction("example/second", testOtherReason)
			second.Rule.Version = 2
			add("new-label", first)
			add("new-label", first)
			add("new-label", second)
			// Effects own their selections after enqueueing.
			first.Types[0] = "com.atproto.moderation.defs#reasonViolation"
			require.NoError(t, eng.persistRecordModActions(c))
			require.Len(t, events, 1)
			assertTargetedEvent(t, events[0], record, "modEventLabel", []string{testSpamReason, testOtherReason}, []RuleMetadata{first.Rule, second.Rule})
			assert.Equal(t, []any{"new-label"}, events[0]["event"].(map[string]any)["createLabelVals"])
		})
	}
}

func TestReportActionSuppression(t *testing.T) {
	for _, scenario := range []string{"quota", "existing-takedown", "existing-account-label", "existing-record-label", "missing-cid", "removal-only", "record-removal-only", "plain-new-label", "quota-with-label"} {
		t.Run(scenario, func(t *testing.T) {
			var events []map[string]any
			eng, c := reportActionFixture(t, captureModEvents(t, []string{"existing", "remove"}, &events))
			action := testReportAction("example/rule", testSpamReason)
			switch scenario {
			case "quota", "quota-with-label":
				eng.Config.QuotaModTakedownDay = 1
				require.NoError(t, eng.Counters.Increment(c.Ctx, "automod-quota", "takedown"))
				c.TakedownAccountWithReportAction(action)
				c.TakedownRecordWithReportAction(action)
				if scenario == "quota-with-label" {
					c.AddAccountLabel("new-label")
				}
			case "existing-takedown":
				c.Account.Takendown = true
				c.TakedownAccountWithReportAction(action)
			case "existing-account-label", "removal-only", "plain-new-label":
				c.Account.AccountLabels = []string{"existing", "remove"}
				c.AddAccountLabelWithReportAction("existing", action)
				if scenario == "removal-only" {
					c.effects.RemoveAccountLabel("remove")
				}
				if scenario == "plain-new-label" {
					c.AddAccountLabel("new-label")
				}
			case "existing-record-label", "record-removal-only":
				c.AddRecordLabelWithReportAction("existing", action)
				if scenario == "record-removal-only" {
					c.effects.RemoveRecordLabel("remove")
				}
			case "missing-cid":
				c.RecordOp.CID = nil
				c.AddRecordLabelWithReportAction("new-label", action)
				c.TakedownRecordWithReportAction(action)
			}
			require.NoError(t, eng.persistRecordModActions(c))
			if scenario == "removal-only" || scenario == "record-removal-only" || scenario == "plain-new-label" || scenario == "quota-with-label" {
				require.Len(t, events, 1)
				assert.NotContains(t, events[0], "reportAction")
				assert.NotContains(t, events[0], "modTool")
				if scenario == "removal-only" || scenario == "record-removal-only" {
					assert.Equal(t, []any{"remove"}, events[0]["event"].(map[string]any)["negateLabelVals"])
				} else {
					assert.Equal(t, []any{"new-label"}, events[0]["event"].(map[string]any)["createLabelVals"])
				}
			} else {
				assert.Empty(t, events)
			}
		})
	}
}

func TestInvalidReportActionStopsPersistence(t *testing.T) {
	invalid := []ReportAction{
		{Rule: RuleMetadata{Name: "plain", Version: 1}, Types: []string{testSpamReason}},
		{Rule: RuleMetadata{Name: "Example/rule", Version: 1}, Types: []string{testSpamReason}},
		{Rule: RuleMetadata{Name: "example/rule", Version: 0}, Types: []string{testSpamReason}},
		{Rule: RuleMetadata{Name: "example/rule", Version: -1}, Types: []string{testSpamReason}},
		testReportAction("example/rule"),
		testReportAction("example/rule", "reasonSpam"),
		testReportAction("example/rule", "com.atproto.moderation.defs#"),
		testReportAction("example/rule", "bad#reasonSpam"),
		testReportAction("example/rule", testSpamReason+"#other"),
		testReportAction("example/rule", testSpamReason+" "),
		testReportAction("example/rule", "com.atproto.moderation.defs#reason/Spam"),
		testReportAction("example/rule", "com.atproto.moderation.defs#1reason"),
	}
	for i, action := range invalid {
		for _, target := range []string{"account-label", "account-takedown", "record-label", "record-takedown"} {
			t.Run(target+"/"+string(rune('a'+i)), func(t *testing.T) {
				eng, c := reportActionFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("invalid configuration sent %s", r.URL.Path) })
				// A valid account effect must not be persisted before invalid record configuration is detected.
				c.TakedownAccount()
				c.AddAccountFlag("new-flag")
				switch target {
				case "account-label":
					c.AddAccountLabelWithReportAction("label", action)
				case "account-takedown":
					c.TakedownAccountWithReportAction(action)
				case "record-label":
					c.AddRecordLabelWithReportAction("label", action)
				case "record-takedown":
					c.TakedownRecordWithReportAction(action)
				}
				require.ErrorContains(t, eng.persistRecordModActions(c), "invalid report action")
				count, err := eng.Counters.GetCount(c.Ctx, "automod-quota", "takedown", countstore.PeriodDay)
				require.NoError(t, err)
				assert.Zero(t, count)
				flags, err := eng.Flags.Get(c.Ctx, testSubjectDID)
				require.NoError(t, err)
				assert.Empty(t, flags)
			})
		}
	}
}

func TestReportActionConcurrentRules(t *testing.T) {
	var events []map[string]any
	eng, c := reportActionFixture(t, captureModEvents(t, nil, &events))
	action := testReportAction("example/rule", testSpamReason)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			c.TakedownAccountWithReportAction(action)
			c.TakedownRecordWithReportAction(action)
			c.AddAccountLabelWithReportAction("label", action)
			c.AddRecordLabelWithReportAction("label", action)
			c.TakedownAccount()
			c.TakedownRecord()
			c.AddAccountLabel("label")
			c.AddRecordLabel("label")
		})
	}
	wg.Wait()
	require.NoError(t, eng.persistRecordModActions(c))
	require.Len(t, events, 4)
	for i, body := range events {
		eventType := "modEventLabel"
		if i%2 == 1 {
			eventType = "modEventTakedown"
		}
		assertTargetedEvent(t, body, i >= 2, eventType, action.Types, []RuleMetadata{action.Rule})
	}
}

func TestReportActionFallback(t *testing.T) {
	const noMatch = "No matching reports found for the specified report types on this subject"
	for _, tc := range []struct {
		name          string
		status        int
		code, message string
		retry         bool
		fallbackFails bool
	}{
		{"exact-no-match", 400, "InvalidRequest", noMatch, true, false},
		{"fallback-fails", 400, "InvalidRequest", noMatch, true, true},
		{"different-message", 400, "InvalidRequest", "invalid subject", false, false},
		{"variant-message", 400, "InvalidRequest", noMatch + ".", false, false},
		{"different-code", 400, "OtherError", noMatch, false, false},
		{"unauthorized", 401, "InvalidRequest", noMatch, false, false},
		{"forbidden", 403, "InvalidRequest", noMatch, false, false},
		{"server-error", 500, "InvalidRequest", noMatch, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []map[string]any
			eng, _ := reportActionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/xrpc/tools.ozone.moderation.emitEvent", r.URL.Path)
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				requests = append(requests, body)
				w.Header().Set("Content-Type", "application/json")
				if len(requests) == 1 || tc.fallbackFails {
					w.WriteHeader(tc.status)
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"error": tc.code, "message": tc.message}))
				} else {
					_, _ = io.WriteString(w, `{}`)
				}
			})
			input := &toolsozone.ModerationEmitEvent_Input{
				CreatedBy: "did:example:moderator",
				Event:     &toolsozone.ModerationEmitEvent_Input_Event{ModerationDefs_ModEventTakedown: &toolsozone.ModerationDefs_ModEventTakedown{}},
				Subject:   &toolsozone.ModerationEmitEvent_Input_Subject{RepoStrongRef: &comatproto.RepoStrongRef{Uri: testRecordURI, Cid: testRecordCID}},
			}
			_, err := emitEventWithReports(context.Background(), eng.OzoneClient, input, []ReportAction{testReportAction("example/rule", testSpamReason)})
			if tc.retry {
				if tc.fallbackFails {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, requests, 2)
				assertTargetedEvent(t, requests[0], true, "modEventTakedown", []string{testSpamReason}, []RuleMetadata{{Name: "example/rule", Version: 1}})
				assert.NotContains(t, requests[1], "reportAction")
				delete(requests[0], "reportAction")
				assert.Equal(t, requests[0], requests[1])
			} else {
				require.Len(t, requests, 1)
				require.Error(t, err)
			}
		})
	}
}
