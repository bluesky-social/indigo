package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.actor.profile

type ActorProfile struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

func (t *ActorProfile) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["description"] = t.Description
	out["displayName"] = t.DisplayName
	return json.Marshal(out)
}
