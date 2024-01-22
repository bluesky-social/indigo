package rules

import (
	"github.com/bluesky-social/indigo/automod"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

var _ automod.BlobRuleFunc = BlobVerifyRule

func BlobVerifyRule(c *automod.RecordContext, blob lexutil.LexBlob, data []byte) error {

	if len(data) == 0 {
		c.AddRecordFlag("empty-blob")
	}

	// check size
	if blob.Size >= 0 && int64(len(data)) != blob.Size {
		c.AddRecordFlag("invalid-blob")
	} else {
		c.Logger.Info("blob checks out", "cid", blob.Ref, "size", blob.Size, "mimetype", blob.MimeType)
	}

	return nil
}
