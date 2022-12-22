package schemagen

// schema: app.bsky.actor.ref

func init() {
}

type ActorRef struct {
	DeclarationCid string `json:"declarationCid" cborgen:"declarationCid"`
	Did            string `json:"did" cborgen:"did"`
}

type ActorRef_WithInfo struct {
	Avatar      *string        `json:"avatar" cborgen:"avatar"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did         string         `json:"did" cborgen:"did"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	Handle      string         `json:"handle" cborgen:"handle"`
}
