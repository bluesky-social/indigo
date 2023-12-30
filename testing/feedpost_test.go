package testing

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"

	"github.com/stretchr/testify/assert"
)

func TestFeedPostParse(t *testing.T) {
	assert := assert.New(t)

	// this is a post-lex-refactor app.bsky.feed.post record
	inFile, err := os.Open("testdata/feedpost_record.cbor")
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

	// marshal as JSON, compare against expected
	expectedJson := `{
		"$type": "app.bsky.feed.post",
		"createdAt": "2023-03-29T20:59:19.417Z",
		"embed": {
			"$type": "app.bsky.embed.recordWithMedia",
			"media": {
				"$type": "app.bsky.embed.images",
				"images": [
					{
						"alt": "",
						"image": {
							"$type": "blob",
							"ref": {
								"$link": "bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlumdylagaqa"
							},
							"mimeType": "image/jpeg",
							"size": 751473
						}
					}
				]
			},
			"record": {
				"$type": "app.bsky.embed.record",
				"record": {
					"cid": "bafyreiaku7udekkiijxcuue3sn6esz7qijqj637rigz4xqdw57fk5houji",
					"uri": "at://did:plc:rbtury4cp2sdk4tvnedaqu54/app.bsky.feed.post/3jilislho4s2k"
				}
			}
		},
		"text": "Who the hell do you think you are"
	}`

	outJsonBytes, err := json.Marshal(fp)
	assert.NoError(err)
	fmt.Println(string(outJsonBytes))
	var outJsonObj map[string]interface{}
	assert.NoError(json.Unmarshal(outJsonBytes, &outJsonObj))
	var expectedJsonObj map[string]interface{}
	assert.NoError(json.Unmarshal([]byte(expectedJson), &expectedJsonObj))
	assert.Equal(expectedJsonObj, outJsonObj)
}

func TestPostToJson(t *testing.T) {
	raw := "a464746578747834e38282e38186e38193e381a3e381a1e3818ce69cace5aeb654776974746572e381a7e38184e38184e381aee381a7e381afefbc9f652474797065726170702e62736b792e666565642e706f737465656d626564a2652474797065756170702e62736b792e656d6265642e696d6167657366696d6167657381a263616c746065696d616765a463726566d82a5825000155122071e37fa09ed1814412a06d4dcd4f9462500b2992c267b9dea11884c52f6bacce6473697a6519ef2e65247479706564626c6f62686d696d65547970656a696d6167652f6a706567696372656174656441747818323032332d30342d30335432323a34363a31392e3438375a"

	b, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}

	var fp bsky.FeedPost
	if err := fp.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}

	outb, err := json.Marshal(&fp)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(outb))
}

// checks a corner-case with $type: "app.bsky.richtext.facet#link"
func TestFeedPostRichtextLink(t *testing.T) {
	assert := assert.New(t)
	cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}

	// this is a app.bsky.feed.post with richtext link
	inFile, err := os.Open("testdata/post_richtext_link.cbor")
	if err != nil {
		t.Fatal(err)
	}
	cborBytes, err := io.ReadAll(inFile)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("=== typescript CBOR bytes (hex)")
	fmt.Println(hex.EncodeToString(cborBytes))
	origCID, err := cidBuilder.Sum(cborBytes)
	if err != nil {
		t.Fatal(err)
	}

	recordCBOR := new(bytes.Buffer)
	var recordOrig appbsky.FeedPost
	var recordRepro appbsky.FeedPost
	assert.NoError(recordOrig.UnmarshalCBOR(bytes.NewReader(cborBytes)))
	assert.Equal("app.bsky.feed.post", recordOrig.LexiconTypeID)

	recordJSON, err := json.Marshal(recordOrig)
	fmt.Println(string(recordJSON))
	assert.NoError(err)
	assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
	assert.Equal(recordOrig, recordRepro)
	assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
	fmt.Println("=== golang cbor-gen bytes (hex)")
	fmt.Println(hex.EncodeToString(recordCBOR.Bytes()))
	reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
	assert.NoError(err)
	assert.Equal(origCID.String(), reproCID.String())

}
