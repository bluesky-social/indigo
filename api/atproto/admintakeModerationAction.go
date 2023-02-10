package atproto

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.takeModerationAction

func init() {
}

type AdminTakeModerationAction_Input struct {
	LexiconTypeID   string                                   `json:"$type,omitempty"`
	Action          string                                   `json:"action" cborgen:"action"`
	CreatedBy       string                                   `json:"createdBy" cborgen:"createdBy"`
	Reason          string                                   `json:"reason" cborgen:"reason"`
	Subject         *AdminTakeModerationAction_Input_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobCids []string                                 `json:"subjectBlobCids,omitempty" cborgen:"subjectBlobCids"`
}

type AdminTakeModerationAction_Input_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoRecordRef *RepoRecordRef
}

func (t *AdminTakeModerationAction_Input_Subject) MarshalJSON() ([]byte, error) {
	if t.RepoRepoRef != nil {
		t.RepoRepoRef.LexiconTypeID = "com.atproto.repo.repoRef"
		return json.Marshal(t.RepoRepoRef)
	}
	if t.RepoRecordRef != nil {
		t.RepoRecordRef.LexiconTypeID = "com.atproto.repo.recordRef"
		return json.Marshal(t.RepoRecordRef)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminTakeModerationAction_Input_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.repo.repoRef":
		t.RepoRepoRef = new(RepoRepoRef)
		return json.Unmarshal(b, t.RepoRepoRef)
	case "com.atproto.repo.recordRef":
		t.RepoRecordRef = new(RepoRecordRef)
		return json.Unmarshal(b, t.RepoRecordRef)

	default:
		return nil
	}
}

func AdminTakeModerationAction(ctx context.Context, c *xrpc.Client, input *AdminTakeModerationAction_Input) (*AdminModerationAction_View, error) {
	var out AdminModerationAction_View
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.takeModerationAction", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
