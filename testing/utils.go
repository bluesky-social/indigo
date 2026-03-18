package testing

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	mathrand "math/rand"
	"os"
	"strings"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
	bsutil "github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

var words = []string{
	"cat",
	"is",
	"cash",
	"dog",
	"bad",
	"system",
	"random",
	"skoot",
	"reply",
	"fish",
	"sunshine",
	"bluesky",
	"make",
	"equal",
	"stars",
	"water",
	"parrot",
}

func MakeRandomPost() string {
	var out []string
	for i := 0; i < 20; i++ {
		out = append(out, words[mathrand.Intn(len(words))])
	}

	return strings.Join(out, " ")
}

var usernames = []string{
	"alice",
	"bob",
	"carol",
	"darin",
	"eve",
	"francis",
	"gerald",
	"hank",
	"ian",
	"jeremy",
	"karl",
	"louise",
	"marion",
	"nancy",
	"oscar",
	"paul",
	"quentin",
	"raul",
	"serena",
	"trevor",
	"ursula",
	"valerie",
	"walter",
	"xico",
	"yousef",
	"zane",
}

func RandSentence(words []string, maxl int) string {
	var out string
	for {
		w := words[mathrand.Intn(len(words))]
		if len(out)+len(w) >= maxl {
			return out
		}

		out = out + " " + w
	}
}

func ReadWords() ([]string, error) {
	b, err := os.ReadFile("/usr/share/dict/words")
	if err != nil {
		return nil, err
	}

	return strings.Split(string(b), "\n"), nil
}

func RandFakeCid() cid.Cid {
	buf := make([]byte, 32)
	rand.Read(buf)

	pref := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
	c, err := pref.Sum(buf)
	if err != nil {
		panic(err)
	}

	return c
}

func RandFakeAtUri(collection, rkey string) string {
	buf := make([]byte, 10)
	rand.Read(buf)
	did := base32.StdEncoding.EncodeToString(buf)

	if rkey == "" {
		rand.Read(buf)
		rkey = base32.StdEncoding.EncodeToString(buf[:6])
	}

	return fmt.Sprintf("at://did:plc:%s/%s/%s", did, collection, rkey)
}

func RandAction() string {
	v := mathrand.Intn(100)
	if v < 40 {
		return "post"
	} else if v < 60 {
		return "repost"
	} else if v < 80 {
		return "reply"
	} else {
		return "like"
	}
}

func GenerateFakeRepo(r *repo.Repo, size int) (cid.Cid, error) {
	words, err := ReadWords()
	if err != nil {
		return cid.Undef, err
	}

	ctx := context.TODO()

	var root cid.Cid
	for i := 0; i < size; i++ {
		switch RandAction() {
		case "post":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Text:      RandSentence(words, 200),
			})
			if err != nil {
				return cid.Undef, err
			}
		case "repost":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.repost", &bsky.FeedRepost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Subject: &atproto.RepoStrongRef{
					Uri: RandFakeAtUri("app.bsky.feed.post", ""),
					Cid: RandFakeCid().String(),
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		case "reply":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Text:      RandSentence(words, 200),
				Reply: &bsky.FeedPost_ReplyRef{
					Root: &atproto.RepoStrongRef{
						Uri: RandFakeAtUri("app.bsky.feed.post", ""),
						Cid: RandFakeCid().String(),
					},
					Parent: &atproto.RepoStrongRef{
						Uri: RandFakeAtUri("app.bsky.feed.post", ""),
						Cid: RandFakeCid().String(),
					},
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		case "like":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.like", &bsky.FeedLike{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Subject: &atproto.RepoStrongRef{
					Uri: RandFakeAtUri("app.bsky.feed.post", ""),
					Cid: RandFakeCid().String(),
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		}

		kmgr := &bsutil.FakeKeyManager{}

		nroot, _, err := r.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			return cid.Undef, err
		}

		root = nroot
	}

	return root, nil
}
