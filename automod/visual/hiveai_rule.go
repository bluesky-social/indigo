package visual

import (
	"strings"

	"github.com/bluesky-social/indigo/automod"
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

	if psclabel == "sfw" {
		if len(labels) > 0 {
			c.Logger.Info("prescreen-safe-failure", "uri", c.RecordOp.ATURI())
		} else {
			c.Logger.Info("prescreen-safe-success", "uri", c.RecordOp.ATURI())
		}
	}

	for _, l := range labels {
		c.AddRecordLabel(l)
	}

	return nil
}
