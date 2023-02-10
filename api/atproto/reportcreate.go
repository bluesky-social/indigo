package atproto

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.report.create

func init() {
}

type ReportCreate_Input struct {
	LexiconTypeID string                      `json:"$type,omitempty"`
	Reason        *string                     `json:"reason,omitempty" cborgen:"reason"`
	ReasonType    *string                     `json:"reasonType" cborgen:"reasonType"`
	Subject       *ReportCreate_Input_Subject `json:"subject" cborgen:"subject"`
}

type ReportCreate_Input_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoRecordRef *RepoRecordRef
}

func (t *ReportCreate_Input_Subject) MarshalJSON() ([]byte, error) {
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
func (t *ReportCreate_Input_Subject) UnmarshalJSON(b []byte) error {
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

type ReportCreate_Output struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	CreatedAt     string                       `json:"createdAt" cborgen:"createdAt"`
	Id            int64                        `json:"id" cborgen:"id"`
	Reason        *string                      `json:"reason,omitempty" cborgen:"reason"`
	ReasonType    *string                      `json:"reasonType" cborgen:"reasonType"`
	ReportedByDid string                       `json:"reportedByDid" cborgen:"reportedByDid"`
	Subject       *ReportCreate_Output_Subject `json:"subject" cborgen:"subject"`
}

type ReportCreate_Output_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoStrongRef *RepoStrongRef
}

func (t *ReportCreate_Output_Subject) MarshalJSON() ([]byte, error) {
	if t.RepoRepoRef != nil {
		t.RepoRepoRef.LexiconTypeID = "com.atproto.repo.repoRef"
		return json.Marshal(t.RepoRepoRef)
	}
	if t.RepoStrongRef != nil {
		t.RepoStrongRef.LexiconTypeID = "com.atproto.repo.strongRef"
		return json.Marshal(t.RepoStrongRef)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *ReportCreate_Output_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.repo.repoRef":
		t.RepoRepoRef = new(RepoRepoRef)
		return json.Unmarshal(b, t.RepoRepoRef)
	case "com.atproto.repo.strongRef":
		t.RepoStrongRef = new(RepoStrongRef)
		return json.Unmarshal(b, t.RepoStrongRef)

	default:
		return nil
	}
}

func ReportCreate(ctx context.Context, c *xrpc.Client, input *ReportCreate_Input) (*ReportCreate_Output, error) {
	var out ReportCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.report.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
