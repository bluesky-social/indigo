package atproto

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.repo.strongRef

func init() {
	util.RegisterType("com.atproto.repo.strongRef#main", &RepoStrongRef{})
}

// RECORDTYPE: RepoStrongRef
type RepoStrongRef struct {
	LexiconTypeID string `json:"$type" cborgen:"$type,const=com.atproto.repo.strongRef"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}
