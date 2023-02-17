package bsky

// schema: app.bsky.actor.ref

func init() {
}

type ActorRef struct {
	LexiconTypeID  string `json:"$type,omitempty"`
	DeclarationCid string `json:"declarationCid" cborgen:"declarationCid"`
	Did            string `json:"did" cborgen:"did"`
}

type ActorRef_ViewerState struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	FollowedBy    *string `json:"followedBy,omitempty" cborgen:"followedBy"`
	Following     *string `json:"following,omitempty" cborgen:"following"`
	Muted         *bool   `json:"muted,omitempty" cborgen:"muted"`
}

type ActorRef_WithInfo struct {
	LexiconTypeID string                `json:"$type,omitempty"`
	Avatar        *string               `json:"avatar,omitempty" cborgen:"avatar"`
	Declaration   *SystemDeclRef        `json:"declaration" cborgen:"declaration"`
	Did           string                `json:"did" cborgen:"did"`
	DisplayName   *string               `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string                `json:"handle" cborgen:"handle"`
	Viewer        *ActorRef_ViewerState `json:"viewer,omitempty" cborgen:"viewer"`
}
