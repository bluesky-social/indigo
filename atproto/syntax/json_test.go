package syntax

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONEncoding(t *testing.T) {
	assert := assert.New(t)

	type AllTogether struct {
		Handle Handle        `json:"handle"`
		Aturi  ATURI         `json:"aturi"`
		Did    DID           `json:"did"`
		Atid   *AtIdentifier `json:"atid"`
		Rkey   RecordKey     `json:"rkey"`
		Col    *NSID         `json:"col"` // demonstrating a pointer
	}
	fullJSON := `{
		"handle": "handle.example.com",
		"aturi": "at://handle.example.com/not.atproto.thing/abc123",
		"did": "did:abc:123",
		"atid": "did:abc:123",
		"rkey": "abc123",
		"col": "not.atproto.thing"
	}`
	assert.Equal(json.Valid([]byte(fullJSON)), true)

	handle, err := ParseHandle("handle.example.com")
	assert.NoError(err)
	aturi, err := ParseATURI("at://handle.example.com/not.atproto.thing/abc123")
	assert.NoError(err)
	did, err := ParseDID("did:abc:123")
	assert.NoError(err)
	atid, err := ParseAtIdentifier("did:abc:123")
	assert.NoError(err)
	rkey, err := ParseRecordKey("abc123")
	assert.NoError(err)
	col, err := ParseNSID("not.atproto.thing")
	assert.NoError(err)

	fullStruct := AllTogether{
		Handle: handle,
		Aturi:  aturi,
		Did:    did,
		Atid:   atid,
		Rkey:   rkey,
		Col:    &col,
	}

	_, err = json.Marshal(fullStruct)
	assert.NoError(err)

	var parseStruct AllTogether
	err = json.Unmarshal([]byte(fullJSON), &parseStruct)
	assert.NoError(err)
	assert.Equal(fullStruct, parseStruct)

	badJSON := `{"handle": 12343}`
	err = json.Unmarshal([]byte(badJSON), &parseStruct)
	assert.Error(err)

	wrongJSON := `{"handle": "asdf"}`
	err = json.Unmarshal([]byte(wrongJSON), &parseStruct)
	assert.Error(err)

	okJSON := `{"handle": "blah.com"}`
	err = json.Unmarshal([]byte(okJSON), &parseStruct)
	assert.NoError(err)
}

func TestJSONHandle(t *testing.T) {
	assert := assert.New(t)

	blob := `["atproto.com", "bsky.app"]`
	var handleList []Handle
	if err := json.Unmarshal([]byte(blob), &handleList); err != nil {
		t.Fatal(err)
	}
	assert.Equal(Handle("atproto.com"), handleList[0])
	assert.Equal(Handle("bsky.app"), handleList[1])
}
