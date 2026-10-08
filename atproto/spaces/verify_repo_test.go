package spaces

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
	carutil "github.com/ipld/go-car/util"
	cbg "github.com/whyrusleeping/cbor-gen"
	"lukechampine.com/blake3"
)

type testRepoRecord struct {
	path string
	cid  cid.Cid
	data []byte
}

type testCARBlock struct {
	cid  cid.Cid
	data []byte
}

type testRepoFixture struct {
	roots   []cid.Cid
	blocks  []testCARBlock
	records []testRepoRecord
	key     atcrypto.PublicKey
	commit  *Commit
}

func testRepoRecords(t *testing.T) []testRepoRecord {
	t.Helper()
	var records []testRepoRecord
	for i, path := range []string{"com.example.record/a", "com.example.record/b", "com.example.record/long"} {
		data := testRecord(t, int64(i+1))
		records = append(records, testRepoRecord{path, testCID(t, data, cid.DagCBOR), data})
	}
	return records
}

// Independent LtHash reference for constructing protocol CAR fixtures. Folding
// uses direct BLAKE3 XOF/little-endian lanes, not the library's LtHash surface.
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

func independentIndex(t *testing.T, records []testRepoRecord) []byte {
	t.Helper()
	var out bytes.Buffer
	cw := cbg.NewCborWriter(&out)
	if err := cw.WriteMajorTypeHeader(cbg.MajMap, uint64(len(records))); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(record.path))); err != nil {
			t.Fatal(err)
		}
		if _, err := out.WriteString(record.path); err != nil {
			t.Fatal(err)
		}
		if err := cbg.WriteCid(cw, record.cid); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func repoFixtureWithIndex(t *testing.T, records []testRepoRecord, index []byte) testRepoFixture {
	t.Helper()
	commit, key := independentCommit(t, referenceIndexHash(t, records))
	commitData := independentCommitCBOR(t, commit, []string{"ikm", "mac", "rev", "sig", "ver", "hash"})
	commitCID, indexCID := testCID(t, commitData, cid.DagCBOR), testCID(t, index, cid.DagCBOR)
	fixture := testRepoFixture{
		roots:   []cid.Cid{commitCID, indexCID},
		blocks:  []testCARBlock{{commitCID, commitData}, {indexCID, index}},
		records: records, key: key, commit: commit,
	}
	for _, record := range records {
		fixture.blocks = append(fixture.blocks, testCARBlock{record.cid, record.data})
	}
	return fixture
}

func repoFixture(t *testing.T, records []testRepoRecord) testRepoFixture {
	t.Helper()
	return repoFixtureWithIndex(t, records, independentIndex(t, records))
}

func independentCAR(t *testing.T, version uint64, roots []cid.Cid, blocks []testCARBlock) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := car.WriteHeader(&car.CarHeader{Roots: roots, Version: version}, &out); err != nil {
		t.Fatal(err)
	}
	for _, block := range blocks {
		if err := carutil.LdWrite(&out, block.cid.Bytes(), block.data); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func fixtureOptions(fixture testRepoFixture) RepoVerificationOptions {
	return RepoVerificationOptions{Space: testSpace, Author: testAuthor, PublicKey: fixture.key}
}

func TestVerifyRepoIndependentCanonicalCAR(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	data := independentCAR(t, 1, fixture.roots, fixture.blocks)
	var received []RecordBlock
	verified, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), func(block RecordBlock) error {
		block.CBOR = bytes.Clone(block.CBOR)
		received = append(received, block)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.RecordsVerified || len(verified.Index) != len(fixture.records) || len(received) != len(fixture.records) {
		t.Fatal("full repo was not verified")
	}
	if !verified.CommitCID.Equals(fixture.roots[0]) || !verified.IndexCID.Equals(fixture.roots[1]) || verified.Commit.Hash != fixture.commit.Hash {
		t.Fatal("verified roots or digest differ")
	}
	for i, record := range fixture.records {
		block := received[i]
		if block.Collection.String()+"/"+block.Rkey.String() != record.path || !block.CID.Equals(record.cid) || !bytes.Equal(block.CBOR, record.data) {
			t.Fatal("records differ or arrived out of index order")
		}
	}
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), nil); err != nil {
		t.Fatal("nil consume should discard verified records", err)
	}
	// Different fields in the expected context must fail before any callback.
	for _, field := range []string{"space", "author", "key"} {
		opts := fixtureOptions(fixture)
		switch field {
		case "space":
			opts.Space = "at://did:web:authority.example/space/com.example.space/other"
		case "author":
			opts.Author = "did:web:other.example"
		case "key":
			private, err := atcrypto.GeneratePrivateKeyP256()
			if err != nil {
				t.Fatal(err)
			}
			opts.PublicKey, err = private.PublicKey()
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), opts, func(RecordBlock) error { t.Fatal("called consume before authentication"); return nil }); err == nil {
			t.Fatalf("accepted wrong %s", field)
		}
	}
}

func TestVerifyRepoIndexOnlyAndEmpty(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	opts := fixtureOptions(fixture)
	opts.IndexOnly = true
	indexOnly := independentCAR(t, 1, fixture.roots, fixture.blocks[:2])
	result, err := VerifyRepo(context.Background(), bytes.NewReader(indexOnly), opts, func(RecordBlock) error { t.Fatal("index-only called consume"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.RecordsVerified || len(result.Index) != len(fixture.records) {
		t.Fatal("index-only result misrepresents record verification")
	}
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(indexOnly), fixtureOptions(fixture), nil); err == nil {
		t.Fatal("full mode accepted index-only CAR")
	}
	for _, blocks := range [][]testCARBlock{fixture.blocks[:3], fixture.blocks} {
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, blocks)), opts, nil); err == nil {
			t.Fatal("index-only mode accepted record blocks")
		}
	}
	empty := repoFixture(t, nil)
	data := independentCAR(t, 1, empty.roots, empty.blocks)
	result, err = VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(empty), nil)
	if err != nil || !result.RecordsVerified || len(result.Index) != 0 {
		t.Fatal("empty full repo verification failed", err)
	}
	if empty.commit.Hash != sha256.Sum256(make([]byte, 2048)) {
		t.Fatal("empty repo fixture hash differs")
	}
}

func TestVerifyRepoMalformedCAR(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	cases := map[string][]byte{}
	cases["one root"] = independentCAR(t, 1, fixture.roots[:1], fixture.blocks)
	cases["three roots"] = independentCAR(t, 1, append(append([]cid.Cid(nil), fixture.roots...), fixture.records[0].cid), fixture.blocks)
	cases["version 2"] = independentCAR(t, 2, fixture.roots, fixture.blocks)
	cases["roots reversed"] = independentCAR(t, 1, []cid.Cid{fixture.roots[1], fixture.roots[0]}, fixture.blocks)
	cases["root blocks reversed"] = independentCAR(t, 1, fixture.roots, append([]testCARBlock{fixture.blocks[1], fixture.blocks[0]}, fixture.blocks[2:]...))
	cases["missing root"] = independentCAR(t, 1, fixture.roots, fixture.blocks[:1])
	cases["missing record"] = independentCAR(t, 1, fixture.roots, fixture.blocks[:len(fixture.blocks)-1])
	cases["extra block"] = independentCAR(t, 1, fixture.roots, append(append([]testCARBlock(nil), fixture.blocks...), fixture.blocks[2]))
	blocks := append([]testCARBlock(nil), fixture.blocks...)
	blocks[2], blocks[3] = blocks[3], blocks[2]
	cases["records reversed"] = independentCAR(t, 1, fixture.roots, blocks)
	for _, i := range []int{0, 1, 2} {
		blocks := append([]testCARBlock(nil), fixture.blocks...)
		blocks[i].data = bytes.Clone(blocks[i].data)
		blocks[i].data[len(blocks[i].data)-1] ^= 1
		cases[[]string{"commit", "index", "record"}[i]+" integrity"] = independentCAR(t, 1, fixture.roots, blocks)
	}
	for _, change := range []string{"version", "revision", "signature", "mac", "hash"} {
		commit := *fixture.commit
		commit.Signature = bytes.Clone(commit.Signature)
		switch change {
		case "version":
			commit.Version = 2
		case "revision":
			commit.Revision = "bad"
		case "signature":
			commit.Signature[0] ^= 1
		case "mac":
			commit.MAC[0] ^= 1
		case "hash":
			commit.Hash[0] ^= 1
			commit.MAC = referenceMAC(commit.IKM, referenceContext(testSpace, testAuthor, commit.Revision, commit.IKM), commit.Hash)
		}
		data := independentCommitCBOR(t, &commit, []string{"ikm", "mac", "rev", "sig", "ver", "hash"})
		c := testCID(t, data, cid.DagCBOR)
		roots := []cid.Cid{c, fixture.roots[1]}
		blocks := append([]testCARBlock(nil), fixture.blocks...)
		blocks[0] = testCARBlock{c, data}
		cases["invalid commit "+change] = independentCAR(t, 1, roots, blocks)
	}
	valid := independentCAR(t, 1, fixture.roots, fixture.blocks)
	for _, suffix := range [][]byte{{0}, {1}, {0x80}, {0x81, 0x80}} {
		cases["trailing framing "+string(suffix)] = append(bytes.Clone(valid), suffix...)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), fixtureOptions(fixture), nil); err == nil {
				t.Fatal("accepted malformed CAR")
			}
		})
	}
	for cut := range len(valid) {
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(valid[:cut]), fixtureOptions(fixture), nil); err == nil {
			t.Fatalf("accepted truncated CAR at %d", cut)
		}
	}
}

func TestVerifyRepoMalformedIndexes(t *testing.T) {
	records := testRepoRecords(t)
	reversed := append([]testRepoRecord(nil), records...)
	reversed[0], reversed[1] = reversed[1], reversed[0]
	duplicate := append([]testRepoRecord(nil), records...)
	duplicate[1].path = duplicate[0].path
	wrongLengthOrder := append([]testRepoRecord(nil), records...)
	wrongLengthOrder[0], wrongLengthOrder[2] = wrongLengthOrder[2], wrongLengthOrder[0]
	for name, entries := range map[string][]testRepoRecord{"byte order": reversed, "duplicate": duplicate, "length order": wrongLengthOrder} {
		t.Run(name, func(t *testing.T) {
			fixture := repoFixture(t, entries)
			if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, fixture.blocks)), fixtureOptions(fixture), nil); err == nil {
				t.Fatal("accepted noncanonical index")
			}
		})
	}
	for _, path := range []string{"invalid/a", "com.example.record/", "com.example.record/../a", "com.example.record/..", "com.example.record/a/b", "/a"} {
		entries := []testRepoRecord{{path, records[0].cid, records[0].data}}
		fixture := repoFixture(t, entries)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, fixture.blocks)), fixtureOptions(fixture), nil); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	wrongProfile := []testRepoRecord{{records[0].path, testCID(t, records[0].data, cid.Raw), records[0].data}}
	fixture := repoFixture(t, wrongProfile)
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, fixture.blocks)), fixtureOptions(fixture), nil); err == nil {
		t.Fatal("accepted index CID with raw profile")
	}
	validIndex := independentIndex(t, records)
	for _, index := range [][]byte{append(bytes.Clone(validIndex), 0), validIndex[:len(validIndex)-1], {0xbf, 0xff}, {0xb8, 3}, {0xba, 0xff, 0xff, 0xff, 0xff}, {0x80}, append([]byte{0xb8, 3}, validIndex[1:]...)} {
		fixture := repoFixtureWithIndex(t, records, index)
		if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, fixture.blocks)), fixtureOptions(fixture), nil); err == nil {
			t.Fatal("accepted malformed index bytes")
		}
	}
	// A root must use DAG-CBOR even if its bytes and digest match a raw CID.
	fixture = repoFixture(t, records)
	raw := testCID(t, fixture.blocks[1].data, cid.Raw)
	fixture.roots[1], fixture.blocks[1].cid = raw, raw
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, fixture.blocks)), fixtureOptions(fixture), nil); err == nil {
		t.Fatal("accepted raw index root")
	}
}

func TestVerifyRepoLimitsCancellationAndPartialCallbacks(t *testing.T) {
	fixture := repoFixture(t, testRepoRecords(t))
	data := independentCAR(t, 1, fixture.roots, fixture.blocks)
	opts := fixtureOptions(fixture)
	opts.MaxBlockSize = 1
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), opts, nil); err == nil {
		t.Fatal("ignored block limit")
	}
	opts.MaxBlockSize = ^uint64(0)
	if _, err := VerifyRepo(context.Background(), bytes.NewReader(data), opts, nil); err == nil {
		t.Fatal("accepted overflowing block limit")
	}
	opts = fixtureOptions(fixture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyRepo(ctx, bytes.NewReader(data), opts, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
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
	// One authenticated callback can precede a failure for a later record.
	blocks := append([]testCARBlock(nil), fixture.blocks...)
	blocks[3].data = []byte("corrupt")
	calls = 0
	result, err := VerifyRepo(context.Background(), bytes.NewReader(independentCAR(t, 1, fixture.roots, blocks)), opts, func(RecordBlock) error { calls++; return nil })
	if err == nil || result != nil || calls != 1 {
		t.Fatal("partial callback trust contract differs", err)
	}
}
