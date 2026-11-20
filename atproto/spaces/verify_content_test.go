package spaces

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

func testCID(t *testing.T, data []byte, codec uint64) cid.Cid {
	t.Helper()
	c, err := cid.NewPrefixV1(codec, multihash.SHA2_256).Sum(data)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testRecord(t *testing.T, value int64) []byte {
	t.Helper()
	data, err := atdata.MarshalCBOR(map[string]any{"$type": "com.example.record", "value": value})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestVerifyRecordIntegrityAndProfile(t *testing.T) {
	data := testRecord(t, 1)
	c := testCID(t, data, cid.DagCBOR)
	if err := VerifyRecord(data, c); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseRecordCID(c.String())
	if err != nil || !parsed.Equals(c) {
		t.Fatal("record CID did not parse", err)
	}
	for _, expected := range []cid.Cid{cid.Undef, testCID(t, data, cid.Raw), testCID(t, testRecord(t, 2), cid.DagCBOR)} {
		if err := VerifyRecord(data, expected); err == nil {
			t.Fatal("accepted invalid record CID or integrity")
		}
	}
	short, err := (cid.Prefix{Version: 1, Codec: cid.DagCBOR, MhType: multihash.SHA2_256, MhLength: 16}).Sum(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecord(data, short); err == nil {
		t.Fatal("accepted truncated digest")
	}
	sha512, err := (cid.Prefix{Version: 1, Codec: cid.DagCBOR, MhType: multihash.SHA2_512, MhLength: 64}).Sum(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecord(data, sha512); err == nil {
		t.Fatal("accepted wrong hash algorithm")
	}
	v0, err := (cid.Prefix{Version: 0, Codec: cid.DagProtobuf, MhType: multihash.SHA2_256, MhLength: 32}).Sum(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRecordCID(v0); err == nil {
		t.Fatal("accepted CIDv0")
	}
	if _, err := parseRecordCID("not-a-cid"); err == nil {
		t.Fatal("accepted lexical garbage")
	}
	// Every malformed datum is assigned its true CID, so failure is about the
	// CBOR/data profile, not merely the content-address check.
	for _, bad := range [][]byte{
		{0x81, 0x01},                             // not an object
		{0xa1, 0x61, 'x', 0xf9, 0x3e, 0x00},      // float 1.5
		{0xa1, 0x61, 'x', 0x18, 0x01},            // nonminimal integer 1
		{0xa2, 0x61, 'b', 0x01, 0x61, 'a', 0x02}, // noncanonical keys
		{0xa2, 0x61, 'a', 0x01, 0x61, 'a', 0x02}, // duplicate keys
		append(bytes.Clone(data), 0),
		data[:len(data)-1],
	} {
		if err := VerifyRecord(bad, testCID(t, bad, cid.DagCBOR)); err == nil {
			t.Fatalf("accepted malformed record %x", bad)
		}
	}
}

type failReader struct{ err error }

func (r failReader) Read([]byte) (int, error) { return 0, r.err }

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type cancelWriter struct {
	cancel context.CancelFunc
	buffer bytes.Buffer
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	n, err := w.buffer.Write(p)
	w.cancel()
	return n, err
}

func TestVerifyBlobStreamingAndErrors(t *testing.T) {
	data := bytes.Repeat([]byte("streaming blob"), 10000)
	c := testCID(t, data, cid.Raw)
	parsed, err := parseBlobCID(c.String())
	if err != nil || !parsed.Equals(c) {
		t.Fatal("blob CID did not parse", err)
	}
	var dst bytes.Buffer
	if err := VerifyBlob(context.Background(), bytes.NewReader(data), &dst, c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Fatal("streamed bytes differ")
	}
	dst.Reset()
	wrong := testCID(t, []byte("different"), cid.Raw)
	if err := VerifyBlob(context.Background(), bytes.NewReader(data), &dst, wrong); err == nil {
		t.Fatal("accepted corrupt blob")
	}
	if dst.Len() != len(data) {
		t.Fatal("integrity failure should leave explicitly provisional output")
	}
	for _, invalid := range []cid.Cid{cid.Undef, testCID(t, data, cid.DagCBOR)} {
		if err := VerifyBlob(context.Background(), bytes.NewReader(data), io.Discard, invalid); err == nil {
			t.Fatal("accepted invalid blob profile")
		}
	}
	readerErr, writerErr := errors.New("reader failure"), errors.New("writer failure")
	if err := VerifyBlob(context.Background(), failReader{readerErr}, io.Discard, c); !errors.Is(err, readerErr) {
		t.Fatal(err)
	}
	if err := VerifyBlob(context.Background(), bytes.NewReader(data), failWriter{writerErr}, c); !errors.Is(err, writerErr) {
		t.Fatal(err)
	}
	if err := VerifyBlob(context.Background(), bytes.NewReader(data), shortWriter{}, c); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyBlob(ctx, bytes.NewReader(data), io.Discard, c); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	writer := &cancelWriter{cancel: cancel}
	if err := VerifyBlob(ctx, bytes.NewReader(data), writer, c); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if writer.buffer.Len() == 0 || writer.buffer.Len() >= len(data) {
		t.Fatal("cancellation did not stop streamed output between writes")
	}
	if err := VerifyBlob(context.Background(), bytes.NewReader(nil), io.Discard, testCID(t, nil, cid.Raw)); err != nil {
		t.Fatal(err)
	}
}
