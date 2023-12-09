package automod

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func MustLoadCapture(capPath string) AccountCapture {
	f, err := os.Open(capPath)
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	var capture AccountCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		panic(err)
	}
	return capture
}

// Test helper which processes all the records from a capture. Intentionally exported, for use in other packages.
//
// This method replaces any pre-existing directory on the engine with a mock directory.
func ProcessCaptureRules(e *Engine, capture AccountCapture) error {
	ctx := context.Background()

	dir := identity.NewMockDirectory()
	dir.Insert(*capture.AccountMeta.Identity)
	e.Directory = &dir

	// initial identity rules
	idevt := IdentityEvent{
		RepoEvent{
			Engine:  e,
			Logger:  e.Logger.With("did", capture.AccountMeta.Identity.DID),
			Account: capture.AccountMeta,
		},
	}
	if err := e.Rules.CallIdentityRules(&idevt); err != nil {
		return err
	}
	if idevt.Err != nil {
		return idevt.Err
	}
	idevt.CanonicalLogLine()
	if err := idevt.PersistActions(ctx); err != nil {
		return err
	}
	if err := idevt.PersistCounters(ctx); err != nil {
		return err
	}

	// all the post rules
	for _, pr := range capture.PostRecords {
		aturi, err := syntax.ParseATURI(pr.Uri)
		if err != nil {
			return err
		}
		path := aturi.Collection().String() + "/" + aturi.RecordKey().String()
		evt := e.NewRecordEvent(capture.AccountMeta, path, pr.Cid, pr.Value.Val)
		e.Logger.Debug("processing record", "did", aturi.Authority(), "path", path)
		if err := e.Rules.CallRecordRules(&evt); err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
		evt.CanonicalLogLine()
		// NOTE: not purging account meta when profile is updated
		if err := evt.PersistActions(ctx); err != nil {
			return err
		}
		if err := evt.PersistCounters(ctx); err != nil {
			return err
		}
	}
	return nil
}

func TestNoOpCaptureReplyRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	capture := MustLoadCapture("testdata/capture_atprotocom.json")
	assert.NoError(ProcessCaptureRules(&engine, capture))
	c, err := engine.GetCount("automod-quota", "report", PeriodDay)
	assert.NoError(err)
	assert.Equal(0, c)
	c, err = engine.GetCount("automod-quota", "takedown", PeriodDay)
	assert.NoError(err)
	assert.Equal(0, c)
}
