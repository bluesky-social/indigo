package mst

// Regression tests for untrusted PrefixLen values in decoded NodeData
// (Linear SEC-4). PrefixLen comes from attacker-controlled CBOR in commit
// CARs and is used as a slice bound against the previous key; before the
// range check it caused a fatal slice-bounds panic on the relay ingest path.

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ipfs/go-cid"
)

func testEntryCid(t *testing.T) cid.Cid {
	c, err := cid.Decode("bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// PrefixLen beyond the length of the previous key (empty for the first
// entry) must return an error, not panic.
func TestNodePrefixLenOutOfRange(t *testing.T) {
	assert := assert.New(t)

	nd := NodeData{
		Entries: []EntryData{{
			PrefixLen: 100,
			KeySuffix: []byte("x"),
			Value:     testEntryCid(t),
		}},
	}
	_, err := nd.Node(nil)
	assert.Error(err)
}

// Negative PrefixLen must return an error, not panic.
func TestNodePrefixLenNegative(t *testing.T) {
	assert := assert.New(t)

	nd := NodeData{
		Entries: []EntryData{{
			PrefixLen: -1,
			KeySuffix: []byte("x"),
			Value:     testEntryCid(t),
		}},
	}
	_, err := nd.Node(nil)
	assert.Error(err)
}

// Huge PrefixLen values (which previously drove a 1TB make() preallocation
// in the legacy package) must be rejected cheaply by the same range check.
func TestNodePrefixLenHuge(t *testing.T) {
	assert := assert.New(t)

	nd := NodeData{
		Entries: []EntryData{{
			PrefixLen: 1 << 40,
			KeySuffix: []byte("x"),
			Value:     testEntryCid(t),
		}},
	}
	_, err := nd.Node(nil)
	assert.Error(err)
}

// Boundary values that are legal must keep working: PrefixLen == 0 on the
// first entry, and PrefixLen == len(prevKey) on a later entry.
func TestNodePrefixLenValidBoundaries(t *testing.T) {
	assert := assert.New(t)

	firstKey := "com.example.record/3jqfcqzm3fo2j"
	nd := NodeData{
		Entries: []EntryData{
			{
				PrefixLen: 0,
				KeySuffix: []byte(firstKey),
				Value:     testEntryCid(t),
			},
			{
				PrefixLen: int64(len(firstKey)),
				KeySuffix: []byte("abc"),
				Value:     testEntryCid(t),
			},
		},
	}
	n, err := nd.Node(nil)
	assert.NoError(err)
	assert.Len(n.Entries, 2)
	assert.Equal([]byte(firstKey), n.Entries[0].Key)
	assert.Equal([]byte(firstKey+"abc"), n.Entries[1].Key)
}

// Out-of-range PrefixLen on a later entry (prevKey non-empty) must also be
// rejected, covering the len(prevKey) upper bound with a non-zero baseline.
func TestNodePrefixLenOutOfRangeLaterEntry(t *testing.T) {
	assert := assert.New(t)

	firstKey := "com.example.record/3jqfcqzm3fo2j"
	nd := NodeData{
		Entries: []EntryData{
			{
				PrefixLen: 0,
				KeySuffix: []byte(firstKey),
				Value:     testEntryCid(t),
			},
			{
				PrefixLen: int64(len(firstKey)) + 1,
				KeySuffix: []byte("x"),
				Value:     testEntryCid(t),
			},
		},
	}
	_, err := nd.Node(nil)
	assert.Error(err)
}
