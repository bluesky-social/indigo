package testing

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/fakedata"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/stretchr/testify/assert"
)

const (
	pdsHost       = "http://localhost:5159"
	adminPassword = "admin"
	celebCount    = 2
	regularCount  = 5
	maxPosts      = 5
	maxFollows    = 5
	// TODO: golang PDS does not support putRecord
	maxMutes    = 0 // 2
	fracMention = 0.3
	// TODO: golang PDS does not support images
	genAvatar = false // true
	genBanner = false // true
	fracImage = 0.0   // 0.3
)

func genTestCatalog(t *testing.T, pdsHost string) fakedata.AccountCatalog {

	ap := adminPassword
	xrpccAdmin := xrpc.Client{
		Client: util.TestingHTTPClient(),
		Host:   pdsHost,
	}
	xrpccAdmin.AdminToken = &ap

	var catalog fakedata.AccountCatalog
	for i := 0; i < celebCount; i++ {
		usr, err := fakedata.GenAccount(&xrpccAdmin, i, "celebrity")
		if err != nil {
			t.Fatal(err)
		}
		catalog.Celebs = append(catalog.Celebs, *usr)
	}
	for i := 0; i < regularCount; i++ {
		usr, err := fakedata.GenAccount(&xrpccAdmin, i, "regular")
		if err != nil {
			t.Fatal(err)
		}
		catalog.Regulars = append(catalog.Regulars, *usr)
	}
	return catalog
}

func TestPDSFakedata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PDS+fakedata test in 'short' test mode")
	}
	assert := assert.New(t)
	plcc := testPLC(t)
	pds := mustSetupPDS(t, "localhost:5159", ".test", plcc)
	pds.Run(t)

	time.Sleep(time.Millisecond * 50)

	catalog := genTestCatalog(t, pdsHost)
	combined := catalog.Combined()
	testClient := util.TestingHTTPClient()

	// generate profile, graph, posts
	for _, acc := range combined {
		xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
		xrpcc.Client = testClient
		if err != nil {
			t.Fatal(err)
		}
		// TODO: golang PDS does not support putRecord
		//assert.NoError(fakedata.GenProfile(xrpcc, &acc, genAvatar, genBanner))
		assert.NoError(fakedata.GenFollowsAndMutes(xrpcc, &catalog, &acc, maxFollows, maxMutes))
		assert.NoError(fakedata.GenPosts(xrpcc, &catalog, &acc, maxPosts, fracImage, fracMention))
	}

	// generate interactions (additional posts, etc)
	for _, acc := range combined {
		xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
		xrpcc.Client = testClient
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(fakedata.GenFollowsAndMutes(xrpcc, &catalog, &acc, maxFollows, maxMutes))
		assert.NoError(fakedata.GenPosts(xrpcc, &catalog, &acc, maxPosts, fracImage, fracMention))
	}

	// do browsing (read-only)
	for _, acc := range combined {
		xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
		xrpcc.Client = testClient
		if err != nil {
			t.Fatal(err)
		}
		// TODO: golang listNotifications broken
		//assert.NoError(fakedata.BrowseAccount(xrpcc, &acc))
	}
}
