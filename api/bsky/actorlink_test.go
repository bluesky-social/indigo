package bsky_test

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

const (
	testBlobCID = "bafkreibme22gw2h7y2h7tg2fhqotaqjucnbc24deqo72b6mkl2egezxhvy"
	testRefCID1 = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"
	testRefCID2 = "bafyreigbtj4x7ip5legnfznufuopl4sg4knzc2cof6duas4b3q2fy6swua"
	testDID     = "did:plc:ewvi7nxzyoun6zhxrhs64oiz"
	testLinkURI = "at://" + testDID + "/app.bsky.actor.link/3m2lp5zsc7422"
)

func strptr(s string) *string { return &s }

func testBlob(t *testing.T) *lexutil.LexBlob {
	c, err := cid.Decode(testBlobCID)
	require.NoError(t, err)
	return &lexutil.LexBlob{Ref: lexutil.LexLink(c), MimeType: "image/png", Size: 1234}
}

func cborBytes(t *testing.T, v interface{ MarshalCBOR(w io.Writer) error }) []byte {
	var buf bytes.Buffer
	require.NoError(t, v.MarshalCBOR(&buf))
	return buf.Bytes()
}

func TestActorLinkJSONDecode(t *testing.T) {
	raw := []byte(`{
		"$type": "app.bsky.actor.link",
		"url": "https://example.com/",
		"title": "Example",
		"icon": {"$type": "blob", "ref": {"$link": "` + testBlobCID + `"}, "mimeType": "image/png", "size": 1234},
		"createdAt": "2026-08-25T12:30:00.000Z",
		"labels": {"$type": "com.atproto.label.defs#selfLabels", "values": [{"val": "porn"}]}
	}`)
	v, err := lexutil.JsonDecodeValue(raw)
	require.NoError(t, err)
	link, ok := v.(*bsky.ActorLink)
	require.True(t, ok, "unexpected type %T", v)
	require.Equal(t, "https://example.com/", link.Url)
	require.Equal(t, "Example", *link.Title)
	require.Equal(t, "2026-08-25T12:30:00.000Z", link.CreatedAt)
	require.Equal(t, testBlobCID, link.Icon.Ref.String())
	require.Equal(t, "image/png", link.Icon.MimeType)
	require.Equal(t, int64(1234), link.Icon.Size)
	require.NotNil(t, link.Labels.LabelDefs_SelfLabels)
	require.Len(t, link.Labels.LabelDefs_SelfLabels.Values, 1)
	require.Equal(t, "porn", link.Labels.LabelDefs_SelfLabels.Values[0].Val)

	minimal, err := lexutil.JsonDecodeValue([]byte(`{"$type":"app.bsky.actor.link","url":"https://example.com/","createdAt":"2026-08-25T12:30:00.000Z"}`))
	require.NoError(t, err)
	m := minimal.(*bsky.ActorLink)
	require.Nil(t, m.Title)
	require.Nil(t, m.Icon)
	require.Nil(t, m.Labels)
}

func TestActorLinkRoundTrip(t *testing.T) {
	full := &bsky.ActorLink{
		LexiconTypeID: "app.bsky.actor.link",
		Url:           "https://example.com/",
		Title:         strptr("Example"),
		Icon:          testBlob(t),
		CreatedAt:     "2026-08-25T12:30:00.000Z",
		Labels: &bsky.ActorLink_Labels{LabelDefs_SelfLabels: &comatproto.LabelDefs_SelfLabels{
			Values: []*comatproto.LabelDefs_SelfLabel{{Val: "porn"}},
		}},
	}
	minimal := &bsky.ActorLink{
		LexiconTypeID: "app.bsky.actor.link",
		Url:           "https://example.com/min",
		CreatedAt:     "2026-08-25T12:30:00.000Z",
	}

	for name, in := range map[string]*bsky.ActorLink{"full": full, "minimal": minimal} {
		t.Run(name, func(t *testing.T) {
			// CBOR
			b := cborBytes(t, in)
			typ, err := lexutil.CborTypeExtract(b)
			require.NoError(t, err)
			require.Equal(t, "app.bsky.actor.link", typ)
			dec, err := lexutil.CborDecodeValue(b)
			require.NoError(t, err)
			out, ok := dec.(*bsky.ActorLink)
			require.True(t, ok, "unexpected type %T", dec)
			require.Equal(t, in.Url, out.Url)
			require.Equal(t, in.CreatedAt, out.CreatedAt)
			require.Equal(t, in.Title, out.Title)
			if in.Icon == nil {
				require.Nil(t, out.Icon)
			} else {
				require.Equal(t, in.Icon.Ref.String(), out.Icon.Ref.String())
				require.Equal(t, in.Icon.MimeType, out.Icon.MimeType)
				require.Equal(t, in.Icon.Size, out.Icon.Size)
			}
			if in.Labels == nil {
				require.Nil(t, out.Labels)
			} else {
				require.Equal(t, "porn", out.Labels.LabelDefs_SelfLabels.Values[0].Val)
			}

			// JSON
			j, err := json.Marshal(in)
			require.NoError(t, err)
			jv, err := lexutil.JsonDecodeValue(j)
			require.NoError(t, err)
			jout := jv.(*bsky.ActorLink)
			require.Equal(t, in.Url, jout.Url)
			require.Equal(t, in.Title, jout.Title)
			if in.Labels != nil {
				require.Equal(t, "porn", jout.Labels.LabelDefs_SelfLabels.Values[0].Val)
			}
			if in.Title == nil {
				var m map[string]any
				require.NoError(t, json.Unmarshal(j, &m))
				for _, k := range []string{"title", "icon", "labels"} {
					require.NotContains(t, m, k)
				}
			}
		})
	}
}

func TestActorProfileLinksRoundTrip(t *testing.T) {
	refs := []*comatproto.RepoStrongRef{
		{Uri: "at://" + testDID + "/app.bsky.actor.link/3m2lp5zsc7422", Cid: testRefCID1},
		{Uri: "at://" + testDID + "/app.bsky.actor.link/3m2lp5zsc7433", Cid: testRefCID2},
	}
	in := &bsky.ActorProfile{
		LexiconTypeID: "app.bsky.actor.profile",
		DisplayName:   strptr("Alice"),
		Links:         refs,
	}

	requireOrdered := func(t *testing.T, got []*comatproto.RepoStrongRef) {
		require.Len(t, got, 2)
		for i := range refs {
			require.Equal(t, refs[i].Uri, got[i].Uri)
			require.Equal(t, refs[i].Cid, got[i].Cid)
		}
	}

	dec, err := lexutil.CborDecodeValue(cborBytes(t, in))
	require.NoError(t, err)
	requireOrdered(t, dec.(*bsky.ActorProfile).Links)

	j, err := json.Marshal(in)
	require.NoError(t, err)
	jv, err := lexutil.JsonDecodeValue(j)
	require.NoError(t, err)
	requireOrdered(t, jv.(*bsky.ActorProfile).Links)

	// Legacy profiles without links omit the field and still decode.
	legacy := &bsky.ActorProfile{LexiconTypeID: "app.bsky.actor.profile", DisplayName: strptr("Bob")}
	lj, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NotContains(t, string(lj), `"links"`)
	lc := cborBytes(t, legacy)
	require.False(t, bytes.Contains(lc, []byte("links")))
	ldec, err := lexutil.CborDecodeValue(lc)
	require.NoError(t, err)
	require.Nil(t, ldec.(*bsky.ActorProfile).Links)
}

func TestProfileViewDetailedLinks(t *testing.T) {
	in := &bsky.ActorDefs_ProfileViewDetailed{
		Did:    testDID,
		Handle: "alice.test",
		Links: []*bsky.ActorDefs_ProfileLinkView{
			{
				Uri:   testLinkURI,
				Cid:   testRefCID1,
				Url:   "https://example.com/",
				Title: strptr("Example"),
				Icon:  strptr("https://cdn.example.com/icon.png"),
				Labels: []*comatproto.LabelDefs_Label{
					{Src: testDID, Uri: testLinkURI, Val: "porn", Cts: "2026-08-25T12:30:00.000Z"},
				},
			},
			{Uri: testLinkURI, Cid: testRefCID2, Url: "https://example.org/"},
		},
	}
	j, err := json.Marshal(in)
	require.NoError(t, err)

	var out bsky.ActorDefs_ProfileViewDetailed
	require.NoError(t, json.Unmarshal(j, &out))
	require.Len(t, out.Links, 2)
	l := out.Links[0]
	require.Equal(t, testLinkURI, l.Uri)
	require.Equal(t, testRefCID1, l.Cid)
	require.Equal(t, "https://example.com/", l.Url)
	require.Equal(t, "Example", *l.Title)
	require.Equal(t, "https://cdn.example.com/icon.png", *l.Icon)
	require.Len(t, l.Labels, 1)
	require.Equal(t, "porn", l.Labels[0].Val)
	require.Equal(t, testDID, l.Labels[0].Src)
	require.Equal(t, "https://example.org/", out.Links[1].Url)
	require.Equal(t, testRefCID2, out.Links[1].Cid)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(j, &raw))
	second := raw["links"].([]any)[1].(map[string]any)
	for _, k := range []string{"title", "icon", "labels"} {
		require.NotContains(t, second, k)
	}

	// Legacy views without links still decode and omit the field.
	legacy := bsky.ActorDefs_ProfileViewDetailed{Did: testDID, Handle: "bob.test"}
	lj, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NotContains(t, string(lj), `"links"`)
	var lout bsky.ActorDefs_ProfileViewDetailed
	require.NoError(t, json.Unmarshal(lj, &lout))
	require.Nil(t, lout.Links)
}
