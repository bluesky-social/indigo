package testing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncScenarios(t *testing.T) {
	assert := assert.New(t)
	ctx := t.Context()

	base, err := LoadScenario(ctx, "testdata/post_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}

	// base case is successful
	assert.NoError(RunScenario(ctx, base))

	// mess with revisions
	// XXX:
	//base.Messages[2].Frame.Event.RepoCommit.Rev = "222pyftdf4h2r"
	//base.Messages[2].Drop = true
	//assert.NoError(RunScenario(ctx, base))
}

func TestWrongKey(t *testing.T) {
	assert := assert.New(t)
	ctx := t.Context()

	base, err := LoadScenario(ctx, "testdata/basic.json")
	if err != nil {
		t.Fatal(err)
	}

	// base case is successful
	//assert.NoError(RunScenario(ctx, base))

	// invalid identity key
	k := base.Accounts[0].Identity.Keys["atproto"]
	k.PublicKeyMultibase = "zQ3shbzd9YoCFQrzfdw2AGpxUHTjUhh69MXRh7hHBavx9wQon"
	base.Accounts[0].Identity.Keys["atproto"] = k
	assert.Error(RunScenario(ctx, base))
}

func TestDeactivationScenario(t *testing.T) {
	ctx := t.Context()

	err := LoadAndRunScenario(ctx, "testdata/deactivation.json")
	if err != nil {
		t.Fatal(err)
	}
}
