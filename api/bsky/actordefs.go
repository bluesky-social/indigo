package bsky

// schema: app.bsky.actor.defs

func init() {
}

type ActorDefs_ProfileView struct {
	LexiconTypeID string                 `json:"$type,omitempty"`
	Avatar        *string                `json:"avatar,omitempty" cborgen:"avatar"`
	Description   *string                `json:"description,omitempty" cborgen:"description"`
	Did           string                 `json:"did" cborgen:"did"`
	DisplayName   *string                `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string                 `json:"handle" cborgen:"handle"`
	IndexedAt     *string                `json:"indexedAt,omitempty" cborgen:"indexedAt"`
	Viewer        *ActorDefs_ViewerState `json:"viewer,omitempty" cborgen:"viewer"`
}

type ActorDefs_ProfileViewBasic struct {
	LexiconTypeID string                 `json:"$type,omitempty"`
	Avatar        *string                `json:"avatar,omitempty" cborgen:"avatar"`
	Did           string                 `json:"did" cborgen:"did"`
	DisplayName   *string                `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string                 `json:"handle" cborgen:"handle"`
	Viewer        *ActorDefs_ViewerState `json:"viewer,omitempty" cborgen:"viewer"`
}

type ActorDefs_ProfileViewDetailed struct {
	LexiconTypeID  string                 `json:"$type,omitempty"`
	Avatar         *string                `json:"avatar,omitempty" cborgen:"avatar"`
	Banner         *string                `json:"banner,omitempty" cborgen:"banner"`
	Description    *string                `json:"description,omitempty" cborgen:"description"`
	Did            string                 `json:"did" cborgen:"did"`
	DisplayName    *string                `json:"displayName,omitempty" cborgen:"displayName"`
	FollowersCount *int64                 `json:"followersCount,omitempty" cborgen:"followersCount"`
	FollowsCount   *int64                 `json:"followsCount,omitempty" cborgen:"followsCount"`
	Handle         string                 `json:"handle" cborgen:"handle"`
	IndexedAt      *string                `json:"indexedAt,omitempty" cborgen:"indexedAt"`
	PostsCount     *int64                 `json:"postsCount,omitempty" cborgen:"postsCount"`
	Viewer         *ActorDefs_ViewerState `json:"viewer,omitempty" cborgen:"viewer"`
}

type ActorDefs_ViewerState struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	FollowedBy    *string `json:"followedBy,omitempty" cborgen:"followedBy"`
	Following     *string `json:"following,omitempty" cborgen:"following"`
	Muted         *bool   `json:"muted,omitempty" cborgen:"muted"`
}
