package search

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestParseEmojis(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(parseEmojis("bunch 🎅 of 🏡 emoji 🤰and 🫄 some 👩‍👩‍👧‍👧 compound"), []string{"🎅", "🏡", "🤰", "🫄", "👩‍👩‍👧‍👧"})

	assert.Equal(parseEmojis("more ⛄ from ☠ lower ⛴ range"), []string{"⛄", "☠", "⛴"})
	assert.True(parseEmojis("blah") == nil)
}

type profileFixture struct {
	DID           string `json:"did"`
	Handle        string `json:"handle"`
	Rkey          string `json:"rkey"`
	Cid           string `json:"cid"`
	DocId         string `json:"doc_id"`
	ProfileRecord *appbsky.ActorProfile
	ProfileDoc    ProfileDoc
}

func TestTransformProfileFixtures(t *testing.T) {
	f, err := os.Open("testdata/transform-profile-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []profileFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		_ = row
		testProfileFixture(t, row)
	}
}

func testProfileFixture(t *testing.T, row profileFixture) {
	assert := assert.New(t)

	repo := identity.Identity{
		Handle: syntax.Handle(row.Handle),
		DID:    syntax.DID(row.DID),
	}
	doc := TransformProfile(row.ProfileRecord, &repo, row.Cid)
	doc.DocIndexTs = "2006-01-02T15:04:05.000Z"
	assert.Equal(row.ProfileDoc, doc)
	assert.Equal(row.DocId, doc.DocId())
}

type postFixture struct {
	DID        string `json:"did"`
	Handle     string `json:"handle"`
	Rkey       string `json:"rkey"`
	Cid        string `json:"cid"`
	DocId      string `json:"doc_id"`
	PostRecord *appbsky.FeedPost
	PostDoc    PostDoc
}

func TestTransformPostFixtures(t *testing.T) {
	f, err := os.Open("testdata/transform-post-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []postFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		_ = row
		testPostFixture(t, row)
	}
}

func testPostFixture(t *testing.T, row postFixture) {
	assert := assert.New(t)

	repo := identity.Identity{
		Handle: syntax.Handle(row.Handle),
		DID:    syntax.DID(row.DID),
	}
	doc := TransformPost(row.PostRecord, repo.DID, row.Rkey, row.Cid)
	doc.DocIndexTs = "2006-01-02T15:04:05.000Z"
	assert.Equal(row.PostDoc, doc)
	assert.Equal(row.DocId, doc.DocId())
}
