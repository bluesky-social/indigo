package atproto

// schema: com.atproto.repo.repoRef

func init() {
}

type RepoRepoRef struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}
