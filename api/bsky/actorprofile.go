package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.actor.profile

// RECORDTYPE: ActorProfile
type ActorProfile struct {
	DisplayName string `json:"displayName" cborgen:"displayName"`
	Description string `json:"description" cborgen:"description"`
}

func (t *ActorProfile) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["description"] = t.Description
	out["displayName"] = t.DisplayName
	return json.Marshal(out)
}
