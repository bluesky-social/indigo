package atproto

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.repo

func init() {
}

type AdminRepo_Account struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Email         string `json:"email" cborgen:"email"`
}

type AdminRepo_Moderation struct {
	LexiconTypeID string                             `json:"$type,omitempty"`
	CurrentAction *AdminModerationAction_ViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
}

type AdminRepo_ModerationDetail struct {
	LexiconTypeID string                             `json:"$type,omitempty"`
	Actions       []*AdminModerationAction_View      `json:"actions" cborgen:"actions"`
	CurrentAction *AdminModerationAction_ViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
	Reports       []*AdminModerationReport_View      `json:"reports" cborgen:"reports"`
}

type AdminRepo_View struct {
	LexiconTypeID  string                    `json:"$type,omitempty"`
	Account        *AdminRepo_Account        `json:"account,omitempty" cborgen:"account"`
	Did            string                    `json:"did" cborgen:"did"`
	Handle         string                    `json:"handle" cborgen:"handle"`
	IndexedAt      string                    `json:"indexedAt" cborgen:"indexedAt"`
	Moderation     *AdminRepo_Moderation     `json:"moderation" cborgen:"moderation"`
	RelatedRecords []util.LexiconTypeDecoder `json:"relatedRecords" cborgen:"relatedRecords"`
}

type AdminRepo_ViewDetail struct {
	LexiconTypeID  string                      `json:"$type,omitempty"`
	Account        *AdminRepo_Account          `json:"account,omitempty" cborgen:"account"`
	Did            string                      `json:"did" cborgen:"did"`
	Handle         string                      `json:"handle" cborgen:"handle"`
	IndexedAt      string                      `json:"indexedAt" cborgen:"indexedAt"`
	Moderation     *AdminRepo_ModerationDetail `json:"moderation" cborgen:"moderation"`
	RelatedRecords []util.LexiconTypeDecoder   `json:"relatedRecords" cborgen:"relatedRecords"`
}
