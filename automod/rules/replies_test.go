package rules

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/engine"

	"github.com/stretchr/testify/assert"
)

func TestIdenticalReplyPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	eng.Rules = automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			IdenticalReplyPostRule,
		},
	}

	capture := engine.MustLoadCapture("testdata/capture_hackerdarkweb.json")
	did := capture.AccountMeta.Identity.DID.String()
	assert.NoError(engine.ProcessCaptureRules(&eng, capture))
	f, err := eng.Flags.Get(ctx, did)
	assert.NoError(err)
	assert.Equal([]string{"multi-identical-reply"}, f)
}
