package rules

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/automod"

	"github.com/stretchr/testify/assert"
)

func TestIdenticalReplyPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	engine := automod.EngineTestFixture()
	engine.Rules = automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			IdenticalReplyPostRule,
		},
	}

	capture := automod.MustLoadCapture("testdata/capture_hackerdarkweb.json")
	did := capture.AccountMeta.Identity.DID.String()
	assert.NoError(automod.ProcessCaptureRules(&engine, capture))
	f, err := engine.Flags.Get(ctx, did)
	assert.NoError(err)
	assert.Equal([]string{"multi-identical-reply"}, f)
}
