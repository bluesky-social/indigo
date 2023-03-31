package atproto

// schema: com.atproto.repo.strongRef

func init() {
}

type RepoStrongRef struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}
