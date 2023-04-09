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
	Action          string                                   `json:"action" cborgen:"action"`
	CreatedBy       string                                   `json:"createdBy" cborgen:"createdBy"`
	Reason          string                                   `json:"reason" cborgen:"reason"`
	Subject         *AdminTakeModerationAction_Input_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobCids []string                                 `json:"subjectBlobCids,omitempty" cborgen:"subjectBlobCids,omitempty"`
}

type AdminTakeModerationAction_Input_Subject struct {
	AdminDefs_RepoRef *AdminDefs_RepoRef
	RepoStrongRef     *RepoStrongRef
}

func (t *AdminTakeModerationAction_Input_Subject) MarshalJSON() ([]byte, error) {
	if t.AdminDefs_RepoRef != nil {
		t.AdminDefs_RepoRef.LexiconTypeID = "com.atproto.admin.defs#repoRef"
		return json.Marshal(t.AdminDefs_RepoRef)
	}
	if t.RepoStrongRef != nil {
		t.RepoStrongRef.LexiconTypeID = "com.atproto.repo.strongRef"
		return json.Marshal(t.RepoStrongRef)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminTakeModerationAction_Input_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.defs#repoRef":
		t.AdminDefs_RepoRef = new(AdminDefs_RepoRef)
		return json.Unmarshal(b, t.AdminDefs_RepoRef)
	case "com.atproto.repo.strongRef":
		t.RepoStrongRef = new(RepoStrongRef)
		return json.Unmarshal(b, t.RepoStrongRef)

	default:
		return nil
	}
}

func AdminTakeModerationAction(ctx context.Context, c *xrpc.Client, input *AdminTakeModerationAction_Input) (*AdminDefs_ActionView, error) {
	var out AdminDefs_ActionView
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.takeModerationAction", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
