package capture

import (
	"testing"

	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/engine"
	"github.com/stretchr/testify/assert"
)

func TestNoOpCaptureReplyRule(t *testing.T) {
	assert := assert.New(t)

	eng := engine.EngineTestFixture()
	capture := MustLoadCapture("testdata/capture_atprotocom.json")
	assert.NoError(ProcessCaptureRules(&eng, capture))
	c, err := eng.GetCount("automod-quota", "report", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(0, c)
	c, err = eng.GetCount("automod-quota", "takedown", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(0, c)
}
