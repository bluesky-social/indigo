package testing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostScenarios(t *testing.T) {
	ctx := context.Background()

	err := LoadAndRunScenario(ctx, "testdata/post_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestWrongKey(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	base, err := LoadScenario(ctx, "testdata/legacy.json")
	if err != nil {
		t.Fatal(err)
	}

	// base case is successful (skipping for speed)
	//assert.NoError(RunScenario(ctx, base))

	// invalid identity key
	k := base.Accounts[0].Identity.Keys["atproto"]
	k.PublicKeyMultibase = "zQ3shbzd9YoCFQrzfdw2AGpxUHTjUhh69MXRh7hHBavx9wQon"
	base.Accounts[0].Identity.Keys["atproto"] = k
	assert.Error(RunScenario(ctx, base))
}

func TestDeactivationScenario(t *testing.T) {
	ctx := context.Background()

	err := LoadAndRunScenario(ctx, "testdata/deactivation.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestRevOrderingScenario(t *testing.T) {
	ctx := context.Background()

	err := LoadAndRunScenario(ctx, "testdata/rev_ordering.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeqOrderingScenario(t *testing.T) {
	ctx := context.Background()

	err := LoadAndRunScenario(ctx, "testdata/seq_ordering.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccountLifecycle(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	base, err := LoadScenario(ctx, "testdata/account_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}

	// base case is successful
	assert.NoError(RunScenario(ctx, base))

	// also works in lenient mode
	base.Lenient = true
	assert.NoError(RunScenario(ctx, base))
}

func TestLegacyScenario(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	base, err := LoadScenario(ctx, "testdata/post_lifecycle_legacy.json")
	if err != nil {
		t.Fatal(err)
	}

	// base case is successful
	assert.NoError(RunScenario(ctx, base))

	// fails in strict mode
	base.Lenient = false
	assert.Error(RunScenario(ctx, base))
}
