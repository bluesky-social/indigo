package schemagen

import (
	"encoding/json"
)

// schema: com.atproto.repo.strongRef

type RepoStrongRef struct {
	Uri string `json:"uri" cborgen:"uri"`
	Cid string `json:"cid" cborgen:"cid"`
}

func (t *RepoStrongRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["uri"] = t.Uri
	return json.Marshal(out)
}
