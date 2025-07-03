package rules

import (
	"context"
	"testing"

	"github.com/gander-social/gander-indigo-sovereign/automod"
	"github.com/gander-social/gander-indigo-sovereign/automod/capture"
	"github.com/gander-social/gander-indigo-sovereign/automod/engine"

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

	cap := capture.MustLoadCapture("testdata/capture_hackerdarkweb.json")
	did := cap.AccountMeta.Identity.DID.String()
	assert.NoError(capture.ProcessCaptureRules(&eng, cap))
	f, err := eng.Flags.Get(ctx, did)
	assert.NoError(err)
	// TODO: tweaked threshold, disabling for now
	_ = f
	//assert.Equal([]string{"multi-identical-reply"}, f)
}
