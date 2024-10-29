package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
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
func ProcessCaptureRules(eng *automod.Engine, capture AccountCapture) error {
	ctx := context.Background()

	did := capture.AccountMeta.Identity.DID
	handle := capture.AccountMeta.Identity.Handle.String()
	dir := identity.NewMockDirectory()
	dir.Insert(*capture.AccountMeta.Identity)
	eng.Directory = &dir

	// initial identity rules
	identEvent := comatproto.SyncSubscribeRepos_Identity{
		Did:    did.String(),
		Handle: &handle,
		Seq:    12345,
		Time:   syntax.DatetimeNow().String(),
	}
	eng.ProcessIdentityEvent(ctx, identEvent)

	// all the post rules
	for _, pr := range capture.PostRecords {
		aturi, err := syntax.ParseATURI(pr.Uri)
		if err != nil {
			return err
		}
		did, err := aturi.Authority().AsDID()
		if err != nil {
			return err
		}
		recCID := syntax.CID(pr.Cid)
		recBuf := new(bytes.Buffer)
		if err := pr.Value.Val.MarshalCBOR(recBuf); err != nil {
			return err
		}
		recBytes := recBuf.Bytes()
		eng.Logger.Debug("processing record", "did", did)
		op := automod.RecordOp{
			Action:     automod.CreateOp,
			DID:        did,
			Collection: aturi.Collection(),
			RecordKey:  aturi.RecordKey(),
			CID:        &recCID,
			RecordCBOR: recBytes,
		}
		eng.ProcessRecordOp(ctx, op)
	}
	return nil
}
