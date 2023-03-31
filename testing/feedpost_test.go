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
	//t.Skip("XXX: currently failing")
	assert := assert.New(t)

	// CBOR file hex (for https://geraintluff.github.io/cbor-debug/):
	// A46474657874782157686F207468652068656C6C20646F20796F75207468696E6B20796F7520617265652474797065726170702E62736B792E666565642E706F737465656D626564A3652474797065781E6170702E62736B792E656D6265642E7265636F7264576974684D65646961656D65646961A2652474797065756170702E62736B792E656D6265642E696D6167657366696D6167657381A263616C746065696D616765A463726566D82A58250155122090873DBDFF81882487CCF6127D8D62F241D6F4F919A73151DABA378580C0806473697A651A0B777165247479706564626C6F62686D696D65547970656A696D6167652F6A706567667265636F7264A2652474797065756170702E62736B792E656D6265642E7265636F7264667265636F7264A263636964783B62616679726569616B75377564656B6B69696A786375756533736E3665737A3771696A716A3633377269677A34787164773537666B35686F756A6963757269784661743A2F2F6469643A706C633A7262747572793463703273646B3474766E656461717535342F6170702E62736B792E666565642E706F73742F336A696C69736C686F3473326B696372656174656441747818323032332D30332D32395432303A35393A31392E3431375A

	// this is a post-lex-refactor app.bsky.feed.post record
	inFile, err := os.Open("feedpost_record.cbor")
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
