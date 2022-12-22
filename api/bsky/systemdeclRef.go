package schemagen

// schema: app.bsky.system.declRef

func init() {
}

type SystemDeclRef struct {
	ActorType string `json:"actorType" cborgen:"actorType"`
	Cid       string `json:"cid" cborgen:"cid"`
}
