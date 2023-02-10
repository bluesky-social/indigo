package atproto

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.record

func init() {
}

type AdminRecord_Moderation struct {
	LexiconTypeID string                             `json:"$type,omitempty"`
	CurrentAction *AdminModerationAction_ViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
}

type AdminRecord_ModerationDetail struct {
	LexiconTypeID string                             `json:"$type,omitempty"`
	Actions       []*AdminModerationAction_View      `json:"actions" cborgen:"actions"`
	CurrentAction *AdminModerationAction_ViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
	Reports       []*AdminModerationReport_View      `json:"reports" cborgen:"reports"`
}

type AdminRecord_View struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	BlobCids      []string                `json:"blobCids" cborgen:"blobCids"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	IndexedAt     string                  `json:"indexedAt" cborgen:"indexedAt"`
	Moderation    *AdminRecord_Moderation `json:"moderation" cborgen:"moderation"`
	Repo          *AdminRepo_View         `json:"repo" cborgen:"repo"`
	Uri           string                  `json:"uri" cborgen:"uri"`
	Value         util.LexiconTypeDecoder `json:"value" cborgen:"value"`
}

type AdminRecord_ViewDetail struct {
	LexiconTypeID string                        `json:"$type,omitempty"`
	Blobs         []*AdminBlob_View             `json:"blobs" cborgen:"blobs"`
	Cid           string                        `json:"cid" cborgen:"cid"`
	IndexedAt     string                        `json:"indexedAt" cborgen:"indexedAt"`
	Moderation    *AdminRecord_ModerationDetail `json:"moderation" cborgen:"moderation"`
	Repo          *AdminRepo_View               `json:"repo" cborgen:"repo"`
	Uri           string                        `json:"uri" cborgen:"uri"`
	Value         util.LexiconTypeDecoder       `json:"value" cborgen:"value"`
}
