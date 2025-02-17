package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/repo/mst"

	"github.com/stretchr/testify/assert"
)

func TestFirehoseTrimTopPartial(t *testing.T) {
	// "failed to invert op: can not prune top of tree: MST is not complete"
	testCommitFile(t, "testdata/firehose_commit_4623075231.json")
}

func TestFirehoseMergePartialNodes(t *testing.T) {
	// "failed to invert op: can't merge partial nodes" (from bridgyfed PDS)
	//testCommitFile(t, "testdata/firehose_commit_4621317030.json")

	// "failed to invert op: can't merge partial nodes" (from bridgyfed PDS)
	//testCommitFile(t, "testdata/firehose_commit_4621317332.json")

	// "failed to invert op: can not merge child nodes: MST is not complete" (from bridgyfed PDS)
	//testCommitFile(t, "testdata/firehose_commit_4621332152.json")
}

func testCommitFile(t *testing.T, p string) {
	assert := assert.New(t)
	ctx := context.Background()

	body, err := os.ReadFile(p)
	assert.NoError(err)
	if err != nil {
		t.Fail()
	}

	var msg comatproto.SyncSubscribeRepos_Commit
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fail()
	}

	_, err = VerifyCommitMessage(ctx, &msg)
	assert.NoError(err)
	if err != nil {
		_, repo, err := LoadFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
		if err != nil {
			t.Fail()
		}
		mst.DebugPrintTree(repo.MST.Root, 0)
	}
}
