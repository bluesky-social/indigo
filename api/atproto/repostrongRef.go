package schemagen

// schema: com.atproto.repo.strongRef

func init() {
}

type RepoStrongRef struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}
