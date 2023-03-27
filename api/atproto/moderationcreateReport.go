package atproto

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.moderation.createReport

func init() {
}

type ModerationCreateReport_Input struct {
	LexiconTypeID string                                `json:"$type,omitempty"`
	Reason        *string                               `json:"reason,omitempty" cborgen:"reason"`
	ReasonType    *string                               `json:"reasonType" cborgen:"reasonType"`
	Subject       *ModerationCreateReport_Input_Subject `json:"subject" cborgen:"subject"`
}

type ModerationCreateReport_Input_Subject struct {
	AdminDefs_RepoRef *AdminDefs_RepoRef
	RepoStrongRef     *RepoStrongRef
}

func (t *ModerationCreateReport_Input_Subject) MarshalJSON() ([]byte, error) {
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
func (t *ModerationCreateReport_Input_Subject) UnmarshalJSON(b []byte) error {
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

type ModerationCreateReport_Output struct {
	LexiconTypeID string                                 `json:"$type,omitempty"`
	CreatedAt     string                                 `json:"createdAt" cborgen:"createdAt"`
	Id            int64                                  `json:"id" cborgen:"id"`
	Reason        *string                                `json:"reason,omitempty" cborgen:"reason"`
	ReasonType    *string                                `json:"reasonType" cborgen:"reasonType"`
	ReportedBy    string                                 `json:"reportedBy" cborgen:"reportedBy"`
	Subject       *ModerationCreateReport_Output_Subject `json:"subject" cborgen:"subject"`
}

type ModerationCreateReport_Output_Subject struct {
	AdminDefs_RepoRef *AdminDefs_RepoRef
	RepoStrongRef     *RepoStrongRef
}

func (t *ModerationCreateReport_Output_Subject) MarshalJSON() ([]byte, error) {
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
func (t *ModerationCreateReport_Output_Subject) UnmarshalJSON(b []byte) error {
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

func ModerationCreateReport(ctx context.Context, c *xrpc.Client, input *ModerationCreateReport_Input) (*ModerationCreateReport_Output, error) {
	var out ModerationCreateReport_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.moderation.createReport", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
