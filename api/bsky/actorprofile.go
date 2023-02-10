package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.actor.profile

func init() {
	util.RegisterType("app.bsky.actor.profile", &ActorProfile{})
}

// RECORDTYPE: ActorProfile
type ActorProfile struct {
	LexiconTypeID string     `json:"$type" cborgen:"$type,const=app.bsky.actor.profile"`
	Avatar        *util.Blob `json:"avatar,omitempty" cborgen:"avatar"`
	Banner        *util.Blob `json:"banner,omitempty" cborgen:"banner"`
	Description   *string    `json:"description,omitempty" cborgen:"description"`
	DisplayName   string     `json:"displayName" cborgen:"displayName"`
}

type ActorProfile_MyState struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Follow        *string `json:"follow,omitempty" cborgen:"follow"`
	Muted         *bool   `json:"muted,omitempty" cborgen:"muted"`
}

type ActorProfile_View struct {
	LexiconTypeID  string                `json:"$type,omitempty"`
	Avatar         *string               `json:"avatar,omitempty" cborgen:"avatar"`
	Banner         *string               `json:"banner,omitempty" cborgen:"banner"`
	Creator        string                `json:"creator" cborgen:"creator"`
	Declaration    *SystemDeclRef        `json:"declaration" cborgen:"declaration"`
	Description    *string               `json:"description,omitempty" cborgen:"description"`
	Did            string                `json:"did" cborgen:"did"`
	DisplayName    *string               `json:"displayName,omitempty" cborgen:"displayName"`
	FollowersCount int64                 `json:"followersCount" cborgen:"followersCount"`
	FollowsCount   int64                 `json:"followsCount" cborgen:"followsCount"`
	Handle         string                `json:"handle" cborgen:"handle"`
	MyState        *ActorProfile_MyState `json:"myState,omitempty" cborgen:"myState"`
	PostsCount     int64                 `json:"postsCount" cborgen:"postsCount"`
}
