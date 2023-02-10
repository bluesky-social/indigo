package atproto

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.blob

func init() {
}

type AdminBlob_ImageDetails struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Height        int64  `json:"height" cborgen:"height"`
	Width         int64  `json:"width" cborgen:"width"`
}

type AdminBlob_Moderation struct {
	LexiconTypeID string                             `json:"$type,omitempty"`
	CurrentAction *AdminModerationAction_ViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
}

type AdminBlob_VideoDetails struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Height        int64  `json:"height" cborgen:"height"`
	Length        int64  `json:"length" cborgen:"length"`
	Width         int64  `json:"width" cborgen:"width"`
}

type AdminBlob_View struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	CreatedAt     string                  `json:"createdAt" cborgen:"createdAt"`
	Details       *AdminBlob_View_Details `json:"details,omitempty" cborgen:"details"`
	MimeType      string                  `json:"mimeType" cborgen:"mimeType"`
	Moderation    *AdminBlob_Moderation   `json:"moderation,omitempty" cborgen:"moderation"`
	Size          int64                   `json:"size" cborgen:"size"`
}

type AdminBlob_View_Details struct {
	AdminBlob_ImageDetails *AdminBlob_ImageDetails
	AdminBlob_VideoDetails *AdminBlob_VideoDetails
}

func (t *AdminBlob_View_Details) MarshalJSON() ([]byte, error) {
	if t.AdminBlob_ImageDetails != nil {
		t.AdminBlob_ImageDetails.LexiconTypeID = "com.atproto.admin.blob#imageDetails"
		return json.Marshal(t.AdminBlob_ImageDetails)
	}
	if t.AdminBlob_VideoDetails != nil {
		t.AdminBlob_VideoDetails.LexiconTypeID = "com.atproto.admin.blob#videoDetails"
		return json.Marshal(t.AdminBlob_VideoDetails)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminBlob_View_Details) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.blob#imageDetails":
		t.AdminBlob_ImageDetails = new(AdminBlob_ImageDetails)
		return json.Unmarshal(b, t.AdminBlob_ImageDetails)
	case "com.atproto.admin.blob#videoDetails":
		t.AdminBlob_VideoDetails = new(AdminBlob_VideoDetails)
		return json.Unmarshal(b, t.AdminBlob_VideoDetails)

	default:
		return nil
	}
}
