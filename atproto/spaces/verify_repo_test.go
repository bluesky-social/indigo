package spaces

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"lukechampine.com/blake3"
)

type testRepoRecord struct {
	path string
	cid  cid.Cid
	data []byte
}

type testRepoFixture struct {
	metadata       []byte
	records        []testRepoRecord
	authentication *MACVerification
	commit         *Commit
}

func testRepoRecords(t *testing.T) []testRepoRecord {
	t.Helper()
	// Independent canonical DRISL: {$type: "com.example.record", value: 1}.
	encoded, err := hex.DecodeString("a265247479706572636f6d2e6578616d706c652e7265636f72646576616c756501")
	if err != nil {
		t.Fatal(err)
	}
	var records []testRepoRecord
	// The first path is longer than the second: STAR sorts bytewise, not by
	// canonical CBOR map-key length as the obsolete index format did.
	for i, path := range []string{"com.example.record/aaaaaaaa", "com.example.record/b", "com.other.record/a"} {
		data := bytes.Clone(encoded)
		data[len(data)-1] = byte(i + 1)
		records = append(records, testRepoRecord{path, testCID(t, data, cid.DagCBOR), data})
	}
	return records
}

// Independent LtHash reference: direct BLAKE3 XOF and little-endian lanes,
// rather than the library's LtHash implementation or any production codecs.
func referenceIndexHash(t *testing.T, records []testRepoRecord) [32]byte {
	t.Helper()
	var lanes [2048]byte
	for _, record := range records {
		h := blake3.New(64, nil)
		h.Write([]byte(record.path + "/" + record.cid.String()))
		var expanded [2048]byte
		if _, err := io.ReadFull(h.XOF(), expanded[:]); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(lanes); i += 2 {
			value := binary.LittleEndian.Uint16(lanes[i:i+2]) + binary.LittleEndian.Uint16(expanded[i:i+2])
			binary.LittleEndian.PutUint16(lanes[i:i+2], value)
		}
	}
	return sha256.Sum256(lanes[:])
}

func repoFixture(t *testing.T, records []testRepoRecord) testRepoFixture {
	t.Helper()
	commit, authentication := independentCommit(t, referenceIndexHash(t, records))
	metadata := independentCommitCBORType(t, commit, []string{"rev", "ver", "data", "repo", "$type", "space"}, "atproto-space-repo")
	return testRepoFixture{metadata, records, authentication, commit}
}

// Construct real STAR framing independently with the standard varint encoder.
func independentSTAR(metadata []byte, records []testRepoRecord) []byte {
	out := []byte{0x2a, 0x6c, 0x01, 0x00}
	out = binary.AppendUvarint(out, uint64(len(metadata)))
	out = append(out, metadata...)
	for _, record := range records {
		out = binary.AppendUvarint(out, uint64(len(record.path)))
		out = append(out, record.path...)
		out = binary.AppendUvarint(out, uint64(len(record.data)))
		out = append(out, record.data...)
	}
	return out
}

func fixtureOptions(fixture testRepoFixture) RepoVerificationOptions {
	return RepoVerificationOptions{Space: testSpace, Author: testAuthor, Authentication: fixture.authentication}
}

func TestVerifyRepoIndependentCanonicalSTAR(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	data := independentSTAR(fixture.metadata, fixture.records)
	var received []RecordBlock
	verified, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), func(block RecordBlock) error {
		block.CBOR = bytes.Clone(block.CBOR)
		received = append(received, block)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Index) != len(fixture.records) || len(received) != len(fixture.records) || *verified.Commit != *fixture.commit {
		t.Fatal("complete repo was not verified")
	}
	for i, record := range fixture.records {
		block, entry := received[i], verified.Index[i]
		if block.Collection.String()+"/"+block.Rkey.String() != record.path || !block.CID.Equals(record.cid) || !bytes.Equal(block.CBOR, record.data) {
			t.Fatal("records differ or arrived out of bytewise path order")
		}
		if entry.Collection != block.Collection || entry.Rkey != block.Rkey || !entry.CID.Equals(block.CID) {
			t.Fatal("returned index does not describe streamed records")
		}
	}
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), nil); err != nil {
		t.Fatal("nil consume should discard records", err)
	}
	for _, field := range []string{"space", "author", "secret", "mac", "missing authentication"} {
		opts := fixtureOptions(fixture)
		auth := *opts.Authentication
		opts.Authentication = &auth
		switch field {
		case "space":
			opts.Space += "2"
		case "author":
			opts.Author = "did:web:other.example"
		case "secret":
			auth.SharedSecret[0] ^= 1
		case "mac":
			auth.MAC[0] ^= 1
		case "missing authentication":
			opts.Authentication = nil
		}
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), opts, func(RecordBlock) error { t.Fatal("consume ran before authentication"); return nil }); err == nil {
			t.Fatalf("accepted wrong %s", field)
		}
	}
}

func TestVerifyRepoIndependentStaticEmptySTAR(t *testing.T) {
	// Entire archive and MAC constructed outside Go using Python struct/hashlib/
	// hmac and direct canonical CBOR, with shared_secret = bytes(range(32)).
	const archive = "2a6c0100b701a6637265766d336d7477777363637571793232637665720164646174615820e5a00aa9991ac8a5ee3109844d84a55583bd20572ad3ffcd42792f3c36b183ad647265706f766469643a7765623a617574686f722e6578616d706c6565247479706572617470726f746f2d73706163652d7265706f657370616365783b61743a2f2f6469643a7765623a617574686f726974792e6578616d706c652f73706163652f636f6d2e6578616d706c652e73706163652f736b6579"
	data, err := hex.DecodeString(archive)
	if err != nil {
		t.Fatal(err)
	}
	mac, err := hex.DecodeString("fcbed43eac73abc42ea2cabf32e3c634c8286565faef374555ba9396f59ddb00")
	if err != nil {
		t.Fatal(err)
	}
	auth := &MACVerification{}
	for i := range auth.SharedSecret {
		auth.SharedSecret[i] = byte(i)
	}
	copy(auth.MAC[:], mac)
	result, err := VerifyRepo(context.Background(), bytes.NewReader(data), RepoVerificationOptions{Space: testSpace, Author: testAuthor, Authentication: auth}, func(RecordBlock) error { t.Fatal("empty repo invoked consume"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Index) != 0 || result.Commit.Data != sha256.Sum256(make([]byte, 2048)) {
		t.Fatal("empty repo verification differs")
	}
}

func TestVerifyRepoMalformedSTARFraming(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	valid := independentSTAR(fixture.metadata, fixture.records)
	header := independentSTAR(fixture.metadata, nil)
	cases := map[string][]byte{
		"CAR v1":                     {0x11, 0xa2, 0x65, 'r', 'o', 'o', 't', 's', 0x80, 0x67, 'v', 'e', 'r', 's', 'i', 'o', 'n', 1},
		"wrong magic":                append([]byte{0x2a, 0x6c, 0x02, 0x00}, valid[4:]...),
		"three byte magic":           append([]byte{0x2a, 0x6c, 0x01}, valid[4:]...),
		"nonminimal metadata length": append(append([]byte{0x2a, 0x6c, 0x01, 0x00}, nonminimalSTARLength(uint64(len(fixture.metadata)))...), valid[len(header)-len(fixture.metadata):]...),
		"seven byte metadata length": {0x2a, 0x6c, 0x01, 0x00, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01},
		"missing all records":        header,
		"extra record":               independentSTAR(fixture.metadata, append(append([]testRepoRecord(nil), fixture.records...), fixture.records[0])),
	}
	first := fixture.records[0]
	pair := binary.AppendUvarint(nil, uint64(len(first.path)))
	pair = append(pair, first.path...)
	for name, suffix := range map[string][]byte{
		"zero path":               {0},
		"truncated path varint":   {0x80},
		"truncated path":          {1},
		"nonminimal path length":  append(nonminimalSTARLength(uint64(len(first.path))), first.path...),
		"missing value length":    pair,
		"truncated value varint":  append(bytes.Clone(pair), 0x80),
		"missing value":           append(bytes.Clone(pair), 1),
		"zero value":              append(bytes.Clone(pair), 0),
		"nonminimal value length": append(append(bytes.Clone(pair), nonminimalSTARLength(uint64(len(first.data)))...), first.data...),
		"seven byte path length":  {0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01},
		"seven byte value length": append(bytes.Clone(pair), 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01),
	} {
		cases[name] = append(bytes.Clone(header), suffix...)
	}
	for _, suffix := range [][]byte{{0}, {1}, {0x80}, {0x81, 0x80}} {
		cases["trailing partial pair "+string(suffix)] = append(bytes.Clone(valid), suffix...)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if result, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), nil); err == nil || result != nil {
				t.Fatal("accepted malformed STAR framing")
			}
		})
	}
	for cut := range len(valid) {
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(valid[:cut]), fixtureOptions(fixture), nil); err == nil {
			t.Fatalf("accepted truncated STAR at %d", cut)
		}
	}
}

func nonminimalSTARLength(value uint64) []byte {
	encoded := binary.AppendUvarint(nil, value)
	encoded[len(encoded)-1] |= 0x80
	return append(encoded, 0)
}

func TestVerifyRepoMalformedMetadata(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	order := []string{"rev", "ver", "data", "repo", "$type", "space"}
	bad := map[string][]byte{
		"missing profile type":        independentCommitCBOR(t, fixture.commit, []string{"rev", "ver", "data", "repo", "space"}),
		"standalone commit type":      independentCommitCBOR(t, fixture.commit, order),
		"obsolete signed commit type": independentCommitCBORType(t, fixture.commit, order, "com.atproto.space.defs#signedCommit"),
		"wrong profile":               independentCommitCBORType(t, fixture.commit, order, "com.example.other"),
		"noncanonical order":          independentCommitCBORType(t, fixture.commit, []string{"ver", "rev", "data", "repo", "$type", "space"}, "atproto-space-repo"),
		"duplicate field":             independentCommitCBORType(t, fixture.commit, []string{"rev", "ver", "data", "repo", "$type", "repo"}, "atproto-space-repo"),
		"nonminimal map header":       append([]byte{0xb8, 6}, fixture.metadata[1:]...),
		"nonminimal integer":          bytes.Replace(fixture.metadata, []byte{0x63, 'v', 'e', 'r', 1}, []byte{0x63, 'v', 'e', 'r', 0x18, 1}, 1),
		"trailing item":               append(bytes.Clone(fixture.metadata), 0),
		"truncated item":              fixture.metadata[:len(fixture.metadata)-1],
		"not a map":                   {0x80},
		"indefinite map":              {0xbf, 0xff},
		"wrong data size":             bytes.Replace(fixture.metadata, []byte{0x58, 0x20}, []byte{0x58, 0x1f}, 1),
	}
	for _, field := range []string{"version", "revision", "repo", "space", "data"} {
		commit := *fixture.commit
		switch field {
		case "version":
			commit.Version = 2
		case "revision":
			commit.Revision = "bad"
		case "repo":
			commit.Repo = "invalid"
		case "space":
			commit.Space = "invalid"
		case "data":
			commit.Data[0] ^= 1 // otherwise-valid metadata with a failed MAC
		}
		bad[field] = independentCommitCBORType(t, &commit, order, "atproto-space-repo")
	}
	for name, metadata := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(metadata, fixture.records)), fixtureOptions(fixture), func(RecordBlock) error { t.Fatal("called consume for invalid metadata"); return nil }); err == nil {
				t.Fatal("accepted malformed STAR metadata")
			}
		})
	}
}

func TestVerifyRepoRecordValidationAndOrder(t *testing.T) {
	records := testRepoRecords(t)
	reversed := append([]testRepoRecord(nil), records...)
	reversed[0], reversed[1] = reversed[1], reversed[0]
	duplicate := append([]testRepoRecord(nil), records...)
	duplicate[1].path = duplicate[0].path
	for name, entries := range map[string][]testRepoRecord{"length rather than byte order": reversed, "duplicate": duplicate} {
		t.Run(name, func(t *testing.T) {
			fixture := repoFixture(t, entries)
			if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, entries)), fixtureOptions(fixture), nil); err == nil {
				t.Fatal("accepted unordered or duplicate paths")
			}
		})
	}
	for _, path := range []string{"invalid/a", "com.example.record/", "com.example.record/../a", "com.example.record/..", "com.example.record/.", "com.example.record/a/b", "/a", "com.example.record/" + strings.Repeat("a", 513), "com.example.record/\xff"} {
		entries := []testRepoRecord{{path, records[0].cid, records[0].data}}
		fixture := repoFixture(t, entries)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, entries)), fixtureOptions(fixture), nil); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	for _, data := range [][]byte{
		{0x81, 1},
		{0xa1, 0x61, 'x', 0xf9, 0x3e, 0x00},
		{0xa1, 0x61, 'x', 0x18, 1},
		{0xa2, 0x61, 'b', 1, 0x61, 'a', 2},
		{0xa2, 0x61, 'a', 1, 0x61, 'a', 2},
		append(bytes.Clone(records[0].data), 0),
		records[0].data[:len(records[0].data)-1],
	} {
		// Commit the true CID of invalid data, so rejection must validate DRISL.
		entries := []testRepoRecord{{records[0].path, testCID(t, data, cid.DagCBOR), data}}
		fixture := repoFixture(t, entries)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, entries)), fixtureOptions(fixture), nil); err == nil {
			t.Fatalf("accepted malformed record %x", data)
		}
	}
	// A different, perfectly canonical record still invalidates the set digest.
	fixture := repoFixture(t, records)
	changed := append([]testRepoRecord(nil), records...)
	changed[1].data = records[0].data
	calls := 0
	result, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, changed)), fixtureOptions(fixture), func(RecordBlock) error { calls++; return nil })
	if err == nil || result != nil || calls != len(records) || !strings.Contains(err.Error(), "digest") {
		t.Fatal("complete callbacks must remain provisional until digest comparison", err)
	}
	// An authenticated but incorrect digest must also fail after all callbacks.
	fixture.commit.Data[0] ^= 1
	fixture.authentication.MAC = referenceMAC(fixture.authentication.SharedSecret, referenceContext(testSpace, testAuthor, testRevision), fixture.commit.Data)
	metadata := independentCommitCBORType(t, fixture.commit, []string{"rev", "ver", "data", "repo", "$type", "space"}, "atproto-space-repo")
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(metadata, records)), fixtureOptions(fixture), nil); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatal("accepted correctly authenticated wrong digest", err)
	}
}

func sizedRepoRecord(t *testing.T, size int) testRepoRecord {
	t.Helper()
	// Canonical {x: bytes(size-8)}, using a four-byte byte-string length.
	data := []byte{0xa1, 0x61, 'x', 0x5a}
	data = binary.BigEndian.AppendUint32(data, uint32(size-8))
	data = append(data, make([]byte, size-8)...)
	return testRepoRecord{"com.example.record/large", testCID(t, data, cid.DagCBOR), data}
}

func TestVerifyRepoSizeLimits(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	opts := fixtureOptions(fixture)
	opts.MaxBlockSize = uint64(len(fixture.metadata) - 1)
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, fixture.records)), opts, nil); err == nil || !strings.Contains(err.Error(), "metadata exceeds") {
		t.Fatal("ignored caller metadata limit", err)
	}
	large := sizedRepoRecord(t, atdata.MAX_CBOR_RECORD_SIZE)
	fixture = repoFixture(t, []testRepoRecord{large})
	for _, limit := range []uint64{0, atdata.MAX_CBOR_RECORD_SIZE, ^uint64(0)} {
		opts = fixtureOptions(fixture)
		opts.MaxBlockSize = limit
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, fixture.records)), opts, nil); err != nil {
			t.Fatal("rejected record at the 1 MiB boundary", err)
		}
	}
	opts.MaxBlockSize = atdata.MAX_CBOR_RECORD_SIZE - 1
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, fixture.records)), opts, nil); err == nil || !strings.Contains(err.Error(), "record exceeds") {
		t.Fatal("ignored tighter caller record limit", err)
	}
	oversized := sizedRepoRecord(t, atdata.MAX_CBOR_RECORD_SIZE+1)
	fixture = repoFixture(t, []testRepoRecord{oversized})
	opts = fixtureOptions(fixture)
	opts.MaxBlockSize = ^uint64(0)
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, fixture.records)), opts, nil); err == nil || !strings.Contains(err.Error(), "record exceeds") {
		t.Fatal("caller limit bypassed protocol's 1 MiB maximum", err)
	}
	// Lengths are rejected before trying to read absent (or allocate huge) data.
	for _, length := range []uint64{atdata.MAX_CBOR_RECORD_SIZE + 1, 64 << 20, 64 << 30} {
		header := binary.AppendUvarint([]byte{0x2a, 0x6c, 0x01, 0x00}, length)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(header), opts, nil); err == nil || !strings.Contains(err.Error(), "metadata exceeds") {
			t.Fatal("metadata limit was not checked before reading", err)
		}
		prefix := independentSTAR(fixture.metadata, nil)
		path := "com.example.record/large"
		prefix = binary.AppendUvarint(prefix, uint64(len(path)))
		prefix = append(prefix, path...)
		prefix = binary.AppendUvarint(prefix, length)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(prefix), opts, nil); err == nil || !strings.Contains(err.Error(), "record exceeds") {
			t.Fatal("record limit was not checked before reading", err)
		}
	}
	header := independentSTAR(fixture.metadata, nil)
	header = binary.AppendUvarint(header, 831)
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(header), opts, nil); err == nil || !strings.Contains(err.Error(), "path length") {
		t.Fatal("path size was not bounded before reading", err)
	}
}

func TestVerifyRepoCancellationErrorsAndPartialCallbacks(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	data := independentSTAR(fixture.metadata, fixture.records)
	opts := fixtureOptions(fixture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyRepo(ctx, bytes.NewReader(data), opts, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := VerifyRepo(context.Background(), nil, opts, nil); err == nil {
		t.Fatal("accepted nil source")
	}
	callbackErr := errors.New("consumer rejected record")
	calls := 0
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), opts, func(RecordBlock) error { calls++; return callbackErr }); !errors.Is(err, callbackErr) || calls != 1 {
		t.Fatal("callback failure not propagated", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	calls = 0
	if _, err := VerifyRepo(ctx, bytes.NewReader(data), opts, func(RecordBlock) error { calls++; cancel(); return nil }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancellation not propagated", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	src := &repoCancelReader{reader: bytes.NewReader(data), cancel: cancel}
	if _, err := VerifyRepo(ctx, src, opts, func(RecordBlock) error { t.Fatal("consume ran after cancellation during read"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("read cancellation not propagated", err)
	}
	readerErr := errors.New("archive source failed")
	for _, reader := range []io.Reader{failReader{readerErr}, io.MultiReader(bytes.NewReader(data), failReader{readerErr})} {
		if result, err := VerifyRepo(context.Background(), reader, opts, nil); !errors.Is(err, readerErr) || result != nil {
			t.Fatal("reader failure not propagated", err)
		}
	}
	changed := append([]testRepoRecord(nil), fixture.records...)
	changed[1].data = []byte("corrupt")
	calls = 0
	result, err := VerifyRepo(context.Background(), bytes.NewReader(independentSTAR(fixture.metadata, changed)), opts, func(RecordBlock) error { calls++; return nil })
	if err == nil || result != nil || calls != 1 {
		t.Fatal("partial callback contract differs", err)
	}
}

type repoCancelReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *repoCancelReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}
