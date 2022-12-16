package schemagen

// schema: app.bsky.actor.ref

func init() {
}

type ActorRef struct {
	Did            string `json:"did" cborgen:"did"`
	DeclarationCid string `json:"declarationCid" cborgen:"declarationCid"`
}

type ActorRef_WithInfo struct {
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	Did         string         `json:"did" cborgen:"did"`
}
