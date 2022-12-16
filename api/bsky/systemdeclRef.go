package schemagen

// schema: app.bsky.system.declRef

func init() {
}

type SystemDeclRef struct {
	Cid       string `json:"cid" cborgen:"cid"`
	ActorType string `json:"actorType" cborgen:"actorType"`
}
