package schemagen

// schema: app.bsky.actor.ref

func init() {
}

type ActorRef struct {
	LexiconTypeID  string `json:"$type,omitempty"`
	DeclarationCid string `json:"declarationCid" cborgen:"declarationCid"`
	Did            string `json:"did" cborgen:"did"`
}

type ActorRef_ViewerState struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Muted         *bool  `json:"muted" cborgen:"muted"`
}

type ActorRef_WithInfo struct {
	LexiconTypeID string                `json:"$type,omitempty"`
	Avatar        *string               `json:"avatar" cborgen:"avatar"`
	Declaration   *SystemDeclRef        `json:"declaration" cborgen:"declaration"`
	Did           string                `json:"did" cborgen:"did"`
	DisplayName   *string               `json:"displayName" cborgen:"displayName"`
	Handle        string                `json:"handle" cborgen:"handle"`
	Viewer        *ActorRef_ViewerState `json:"viewer" cborgen:"viewer"`
}
