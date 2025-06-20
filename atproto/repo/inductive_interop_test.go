package repo

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/gander-social/gander-indigo-sovereign/atproto/repo/mst"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/stretchr/testify/assert"
)

type CommitProofFixture struct {
	Comment          string   `json:"comment"`
	LeafValue        string   `json:"leafValue"`
	Keys             []string `json:"keys"`
	Additions        []string `json:"adds"`
	Deletions        []string `json:"dels"`
	RootBeforeCommit string   `json:"rootBeforeCommit"`
	RootAfterCommit  string   `json:"rootAfterCommit"`
	BlocksInProof    []string `json:"blocksInProof"`
}

func LoadCommitProofFixtures(p string) []CommitProofFixture {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	var fixtures []CommitProofFixture
	if err := json.Unmarshal(b, &fixtures); err != nil {
		panic(err)
	}
	return fixtures
}

func (f *CommitProofFixture) RecordCID() cid.Cid {
	c, err := cid.Decode(f.LeafValue)
	if err != nil {
		panic(err)
	}
	return c
}

func (f *CommitProofFixture) Tree() mst.Tree {
	m := map[string]cid.Cid{}
	c := f.RecordCID()
	for _, k := range f.Keys {
		m[k] = c
	}
	tree, err := mst.LoadTreeFromMap(m)
	if err != nil {
		panic(err)
	}
	return *tree
}

func (f *CommitProofFixture) Operations() []Operation {
	c := f.RecordCID()
	ops := []Operation{}
	for _, key := range f.Additions {
		ops = append(ops, Operation{
			Path:  key,
			Value: &c,
			Prev:  nil,
		})
	}
	for _, key := range f.Deletions {
		ops = append(ops, Operation{
			Path:  key,
			Value: nil,
			Prev:  &c,
		})
	}
	return ops
}

func (f *CommitProofFixture) Test(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	tree := f.Tree()
	ops := f.Operations()

	// verify "before" tree CID
	before, err := tree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(f.RootBeforeCommit, before.String())
	assert.NoError(tree.Verify())

	// apply all ops, and generate diff
	for _, o := range ops {
		_, err := ApplyOp(&tree, o.Path, o.Value)
		if err != nil {
			t.Fatal(err)
		}
	}
	diffBlocks := blockstore.NewBlockstore(datastore.NewMapDatastore())
	diffRoot, err := tree.WriteDiffBlocks(ctx, diffBlocks)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(f.RootAfterCommit, diffRoot.String())
	diffTree, err := mst.LoadTreeFromStore(ctx, diffBlocks, *diffRoot)
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(diffTree.Verify())

	// verify inclusion of blocks in diff
	for _, cstr := range f.BlocksInProof {
		c, err := cid.Decode(cstr)
		if err != nil {
			t.Fatal(err)
		}
		_, err = diffBlocks.Get(ctx, c)
		assert.NoError(err)
	}

	// invert all ops
	for _, o := range ops {
		assert.NoError(CheckOp(diffTree, &o))
		assert.NoError(InvertOp(diffTree, &o))
	}

	// verify "before" tree CID
	checkCID, err := diffTree.RootCID()
	assert.NoError(err)
	assert.Equal(f.RootBeforeCommit, checkCID.String())
}

func TestCommitProofFixtures(t *testing.T) {
	fixtures := LoadCommitProofFixtures("testdata/commit-proof-fixtures.json")

	for _, f := range fixtures {
		f.Test(t)
	}
}
