package visual

import (
	"strings"
	"time"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/rules"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

func (hal *HiveAIClient) HiveLabelBlobRule(c *automod.RecordContext, blob lexutil.LexBlob, data []byte) error {

	if !strings.HasPrefix(blob.MimeType, "image/") {
		return nil
	}

	var psclabel string
	if hal.PreScreenClient != nil {
		val, err := hal.PreScreenClient.PreScreenImage(c.Ctx, data)
		if err != nil {
			c.Logger.Info("prescreen-request-error", "err", err)
		} else {
			psclabel = val
		}
	}

	labels, err := hal.LabelBlob(c.Ctx, blob, data)
	if err != nil {
		return err
	}

	if hal.PreScreenClient != nil {
		if psclabel == "sfw" {
			if len(labels) > 0 {
				c.Logger.Info("prescreen-safe-failure", "uri", c.RecordOp.ATURI())
			} else {
				c.Logger.Info("prescreen-safe-success", "uri", c.RecordOp.ATURI())
			}
		} else {
			c.Logger.Info("prescreen-nsfw", "uri", c.RecordOp.ATURI())
		}
	}

	for _, l := range labels {
		// NOTE: experimenting with profile reporting for new accounts
		if l == "sexual" && c.RecordOp.Collection.String() == "app.bsky.actor.profile" && rules.AccountIsYoungerThan(&c.AccountContext, 2*24*time.Hour) {
			c.ReportRecord(automod.ReportReasonSexual, "possible sexual profile (not labeled yet)")
			c.Logger.Info("skipping record label", "label", l, "reason", "sexual-profile-experiment")
		} else {
			c.AddRecordLabel(l)
		}
	}

	return nil
}
