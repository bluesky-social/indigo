package atproto

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.sync.subscribeAllRepos

func init() {
}

type SyncSubscribeAllRepos_RepoAppend struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Blobs         []string                `json:"blobs" cborgen:"blobs"`
	Blocks        util.LexiconTypeDecoder `json:"blocks" cborgen:"blocks"`
	Commit        string                  `json:"commit" cborgen:"commit"`
	Prev          *string                 `json:"prev,omitempty" cborgen:"prev"`
	Repo          string                  `json:"repo" cborgen:"repo"`
	Time          string                  `json:"time" cborgen:"time"`
}

type SyncSubscribeAllRepos_RepoRebase struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Commit        string `json:"commit" cborgen:"commit"`
	Repo          string `json:"repo" cborgen:"repo"`
	Time          string `json:"time" cborgen:"time"`
}
