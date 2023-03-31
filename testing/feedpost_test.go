package testing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"

	"github.com/stretchr/testify/assert"
)

func TestFeedPostParse(t *testing.T) {
	assert := assert.New(t)

	// this is a post-lex-refactor app.bsky.feed.post record
	inFile, err := os.Open("test_files/feedpost_record.cbor")
	assert.NoError(err)
	cborBytes, err := io.ReadAll(inFile)
	assert.NoError(err)

	var fp appbsky.FeedPost
	assert.NoError(fp.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	assert.Equal("app.bsky.feed.post", fp.LexiconTypeID)
	assert.Equal("Who the hell do you think you are", fp.Text)
	assert.Equal("2023-03-29T20:59:19.417Z", fp.CreatedAt)
	assert.Nil(fp.Entities)
	assert.Nil(fp.Facets)
	assert.Nil(fp.Reply)
	assert.Nil(fp.Embed.EmbedImages)
	assert.Nil(fp.Embed.EmbedExternal)
	assert.Nil(fp.Embed.EmbedRecord)
	assert.NotNil(fp.Embed.EmbedRecordWithMedia)
	assert.Equal("app.bsky.embed.recordWithMedia", fp.Embed.EmbedRecordWithMedia.LexiconTypeID)

	cc, err := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlumdylagaqa")
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(
		&appbsky.EmbedRecordWithMedia{
			LexiconTypeID: "app.bsky.embed.recordWithMedia",
			Media: &appbsky.EmbedRecordWithMedia_Media{
				EmbedImages: &appbsky.EmbedImages{
					LexiconTypeID: "app.bsky.embed.images",
					Images: []*appbsky.EmbedImages_Image{
						&appbsky.EmbedImages_Image{
							Image: &lexutil.LexBlob{
								//LexiconTypeID: "blob",
								Ref:      lexutil.LexLink(cc), // 000155122090873DBDFF810882487CCF6127D8D62F241D6F4F919A73151DABA3078580C080
								Size:     751473,
								MimeType: "image/jpeg",
							},
						},
					},
				},
			},
			Record: &appbsky.EmbedRecord{
				LexiconTypeID: "app.bsky.embed.record",
				Record: &comatproto.RepoStrongRef{
					Cid: "bafyreiaku7udekkiijxcuue3sn6esz7qijqj637rigz4xqdw57fk5houji",
					Uri: "at://did:plc:rbtury4cp2sdk4tvnedaqu54/app.bsky.feed.post/3jilislho4s2k",
				},
			},
		},
		fp.Embed.EmbedRecordWithMedia,
	)

	// re-encode as CBOR, check against input bytes
	outCborBytes := new(bytes.Buffer)
	assert.NoError(fp.MarshalCBOR(outCborBytes))
	assert.Equal(cborBytes, outCborBytes.Bytes())

	fmt.Printf("OUTPUT: %x\n", outCborBytes.Bytes())

	// marshal as JSON
	outJsonBytes, err := json.Marshal(fp)
	assert.NoError(err)
	var outJson map[string]interface{}
	assert.NoError(json.Unmarshal(outJsonBytes, &outJson))
	assert.Equal("app.bsky.feed.post", outJson["$type"])
	// TODO: more
}
