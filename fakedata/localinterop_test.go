//go:build localinterop

package fakedata

import (
	"testing"

	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/stretchr/testify/assert"
)

const (
	pdsHost       = "http://localhost:2583"
	adminPassword = "admin"
	celebCount    = 2
	regularCount  = 5
	maxPosts      = 5
	maxFollows    = 5
	maxMutes      = 2
	fracImage     = 0.3
	fracMention   = 0.3
)

func genTestCatalog(t *testing.T, pdsHost string) AccountCatalog {

	ap := adminPassword
	xrpccAdmin := xrpc.Client{
		Client: util.TestingHTTPClient(),
		Host:   pdsHost,
	}
	xrpccAdmin.AdminToken = &ap

	var catalog AccountCatalog
	for i := 0; i < celebCount; i++ {
		usr, err := GenAccount(&xrpccAdmin, i, "celebrity")
		if err != nil {
			t.Fatal(err)
		}
		catalog.Celebs = append(catalog.Celebs, *usr)
	}
	for i := 0; i < regularCount; i++ {
		usr, err := GenAccount(&xrpccAdmin, i, "regular")
		if err != nil {
			t.Fatal(err)
		}
		catalog.Regulars = append(catalog.Regulars, *usr)
	}
	return catalog
}

// runs basic fakedata generation against a local PDS process. eg, the
// typescript implementation, running locally or in docker
func TestFakedataLocalInterop(t *testing.T) {
	assert := assert.New(t)
	_ = assert

	catalog := genTestCatalog(t, pdsHost)
	combined := catalog.Combined()

	// generate profile, graph, posts
	for _, acc := range combined {
		xrpcc, err := AccountXrpcClient(pdsHost, &acc)
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(GenProfile(xrpcc, &acc, true, true))
		assert.NoError(GenFollowsAndMutes(xrpcc, &catalog, &acc, maxFollows, maxMutes))
		assert.NoError(GenPosts(xrpcc, &catalog, &acc, maxPosts, fracImage, fracMention))
	}

	// generate interactions (additional posts, etc)
	for _, acc := range combined {
		xrpcc, err := AccountXrpcClient(pdsHost, &acc)
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(GenFollowsAndMutes(xrpcc, &catalog, &acc, maxFollows, maxMutes))
		assert.NoError(GenPosts(xrpcc, &catalog, &acc, maxPosts, fracImage, fracMention))
	}

	// do browsing (read-only)
	for _, acc := range combined {
		xrpcc, err := AccountXrpcClient(pdsHost, &acc)
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(BrowseAccount(xrpcc, &acc))
	}
}
