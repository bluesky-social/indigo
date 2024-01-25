package visual

import (
	"strings"

	"github.com/bluesky-social/indigo/automod"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

func (ac *AbyssClient) AbyssScanBlobRule(c *automod.RecordContext, blob lexutil.LexBlob, data []byte) error {

	if !strings.HasPrefix(blob.MimeType, "image/") {
		return nil
	}

	params := make(map[string]string)
	params["did"] = c.Account.Identity.DID.String()
	if !c.Account.Identity.Handle.IsInvalidHandle() {
		params["handle"] = c.Account.Identity.Handle.String()
	}
	if c.Account.Private != nil && c.Account.Private.Email != "" {
		params["accountEmail"] = c.Account.Private.Email
	}
	params["uri"] = c.RecordOp.ATURI().String()

	resp, err := ac.ScanBlob(c.Ctx, blob, data, params)
	if err != nil {
		return err
	}

	if resp.Match == nil || resp.Match.Status != "success" {
		// TODO: should this return an error, or just log?
		c.Logger.Error("abyss blob scan failed", "cid", blob.Ref.String())
		return nil
	}

	if resp.Match.IsAbuseMatch() {
		c.Logger.Warn("abyss blob match", "cid", blob.Ref.String())
		c.AddRecordFlag("abyss-match")
		c.TakedownRecord()
		// purge blob as part of record takedown
		c.TakedownBlob(blob.Ref.String())
		c.ReportRecord(automod.ReportReasonViolation, "possible CSAM image match; post has been takendown while verifying.\nAccount should be reviewed for any other content")
	}

	return nil
}
