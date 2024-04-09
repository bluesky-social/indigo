//go:build localsearch

package search

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
	es "github.com/opensearch-project/opensearch-go/v2"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	testPostIndex    = "palomar_test_post"
	testProfileIndex = "palomar_test_profile"
)

func testEsClient(t *testing.T) *es.Client {
	cfg := es.Config{
		Addresses: []string{"http://localhost:9200"},
		Username:  "admin",
		Password:  "0penSearch-Pal0mar",
		CACert:    nil,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 5,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	escli, err := es.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	info, err := escli.Info()
	if err != nil {
		t.Fatal(err)
	}
	info.Body.Close()
	return escli

}

func testServer(ctx context.Context, t *testing.T, escli *es.Client, dir identity.Directory) *Server {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(
		db,
		escli,
		dir,
		Config{
			RelayHost:           "wss://relay.invalid",
			PostIndex:           testPostIndex,
			ProfileIndex:        testProfileIndex,
			Logger:              slog.Default(),
			RelaySyncRateLimit:  1,
			IndexMaxConcurrency: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// NOTE: skipping errors
	resp, _ := srv.escli.Indices.Delete([]string{testPostIndex, testProfileIndex})
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	if err := srv.EnsureIndices(ctx); err != nil {
		t.Fatal(err)
	}

	return srv
}

func TestJapaneseRegressions(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	escli := testEsClient(t)
	dir := identity.NewMockDirectory()
	srv := testServer(ctx, t, escli, &dir)
	ident := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}

	res, err := DoSearchPosts(ctx, &dir, escli, testPostIndex, "english", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(0, len(res.Hits.Hits))

	p1 := appbsky.FeedPost{Text: "basic english post", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p1, "app.bsky.feed.post/3kpnillluoh2y", cid.Undef))

	// https://github.com/bluesky-social/indigo/issues/302
	p2 := appbsky.FeedPost{Text: "学校から帰って熱いお風呂に入ったら力一杯がんばる", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p2, "app.bsky.feed.post/3kpnillluo222", cid.Undef))
	p3 := appbsky.FeedPost{Text: "熱力学", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p3, "app.bsky.feed.post/3kpnillluo333", cid.Undef))
	p4 := appbsky.FeedPost{Text: "東京都", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p4, "app.bsky.feed.post/3kpnillluo444", cid.Undef))
	p5 := appbsky.FeedPost{Text: "京都", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p5, "app.bsky.feed.post/3kpnillluo555", cid.Undef))
	p6 := appbsky.FeedPost{Text: "パリ", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p6, "app.bsky.feed.post/3kpnillluo666", cid.Undef))
	p7 := appbsky.FeedPost{Text: "ハリー・ポッター", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p7, "app.bsky.feed.post/3kpnillluo777", cid.Undef))
	p8 := appbsky.FeedPost{Text: "ハリ", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p8, "app.bsky.feed.post/3kpnillluo223", cid.Undef))
	p9 := appbsky.FeedPost{Text: "multilingual 多言語", CreatedAt: "2024-01-02T03:04:05.006Z"}
	assert.NoError(srv.indexPost(ctx, &ident, &p9, "app.bsky.feed.post/3kpnillluo224", cid.Undef))

	_, err = srv.escli.Indices.Refresh()
	assert.NoError(err)

	// expect all to be indexed
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "*", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(9, len(res.Hits.Hits))

	// check that english matches (single post)
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "english", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	// "thermodynamics"; should return only one match
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "熱力学", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	// "Kyoto"; should return only one match
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "京都", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	// "Paris"; should return only one match
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "パリ", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	// should return only one match
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "ハリー", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	// part of a word; should match none
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "ハ", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(0, len(res.Hits.Hits))

	// should match both ways, and together
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "multilingual", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))

	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "多言語", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "multilingual 多言語", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))
	res, err = DoSearchPosts(ctx, &dir, escli, testPostIndex, "\"multilingual 多言語\"", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(res.Hits.Hits))
}
