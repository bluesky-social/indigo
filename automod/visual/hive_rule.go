package visual

import (
	"strings"

	"github.com/bluesky-social/indigo/automod"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

func (hal *HiveAILabeler) HiveLabelBlobRule(c *automod.RecordContext, blob lexutil.LexBlob, data []byte) error {

	if !strings.HasPrefix(blob.MimeType, "image/") {
		return nil
	}

	labels, err := hal.LabelBlob(c.Ctx, blob, data)
	if err != nil {
		return err
	}

	for _, l := range labels {
		c.AddRecordLabel(l)
	}

	return nil
}
