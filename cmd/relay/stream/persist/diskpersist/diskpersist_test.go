package diskpersist

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/stream"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testCid() cid.Cid {
	buf := make([]byte, 32)
	c, err := cid.NewPrefixV1(cid.Raw, mh.SHA2_256).Sum(buf)
	if err != nil {
		panic(err)
	}
	return c
}

func TestInitialSequenceNumber(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()

	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "relay.sqlite")))
	require.NoError(err)

	// Open the persistence, the current sequence should be 1 (the default):
	persist, err := NewDiskPersistence(dir, "", db, DefaultDiskPersistOptions())
	require.NoError(err)
	require.Equal(int64(1), persist.curSeq)

	// Shutdown the persister:
	err = persist.Shutdown(t.Context())
	require.NoError(err)

	// Reopen the persistence, the current sequence should still be 1:
	persist, err = NewDiskPersistence(dir, "", db, DefaultDiskPersistOptions())
	require.NoError(err)
	require.Equal(int64(1), persist.curSeq)

	// Fake the DID to UID mapping:
	did := "did:example:123"
	persist.didCache.Add(did, 123)

	// Insert a dummy event:
	event := &stream.XRPCStreamEvent{
		RepoCommit: &atproto.SyncSubscribeRepos_Commit{
			Repo:   did,
			Commit: lexutil.LexLink(testCid()),
			Time:   time.Now().Format(util.ISO8601),
		},
	}
	err = persist.Persist(t.Context(), event)
	require.NoError(err)

	// Sequence number should now be 2:
	require.Equal(int64(2), persist.curSeq)

	// Shutdown the persister:
	err = persist.Shutdown(t.Context())
	require.NoError(err)

	// Reopen the persistence, the current sequence should still be 2:
	persist, err = NewDiskPersistence(dir, "", db, DefaultDiskPersistOptions())
	require.NoError(err)
	require.Equal(int64(2), persist.curSeq)
}
