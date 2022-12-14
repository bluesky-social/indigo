package schemagen

// schema: com.atproto.repo.strongRef

func init() {
}

type RepoStrongRef struct {
	Cid string `json:"cid" cborgen:"cid"`
	Uri string `json:"uri" cborgen:"uri"`
}
