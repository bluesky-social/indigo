package atproto

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.moderationReport

func init() {
}

type AdminModerationReport_View struct {
	LexiconTypeID       string                              `json:"$type,omitempty"`
	CreatedAt           string                              `json:"createdAt" cborgen:"createdAt"`
	Id                  int64                               `json:"id" cborgen:"id"`
	Reason              *string                             `json:"reason,omitempty" cborgen:"reason"`
	ReasonType          *string                             `json:"reasonType" cborgen:"reasonType"`
	ReportedByDid       string                              `json:"reportedByDid" cborgen:"reportedByDid"`
	ResolvedByActionIds []int64                             `json:"resolvedByActionIds" cborgen:"resolvedByActionIds"`
	Subject             *AdminModerationReport_View_Subject `json:"subject" cborgen:"subject"`
}

type AdminModerationReport_ViewDetail struct {
	LexiconTypeID     string                                    `json:"$type,omitempty"`
	CreatedAt         string                                    `json:"createdAt" cborgen:"createdAt"`
	Id                int64                                     `json:"id" cborgen:"id"`
	Reason            *string                                   `json:"reason,omitempty" cborgen:"reason"`
	ReasonType        *string                                   `json:"reasonType" cborgen:"reasonType"`
	ReportedByDid     string                                    `json:"reportedByDid" cborgen:"reportedByDid"`
	ResolvedByActions []*AdminModerationAction_View             `json:"resolvedByActions" cborgen:"resolvedByActions"`
	Subject           *AdminModerationReport_ViewDetail_Subject `json:"subject" cborgen:"subject"`
}

type AdminModerationReport_ViewDetail_Subject struct {
	AdminRepo_View   *AdminRepo_View
	AdminRecord_View *AdminRecord_View
}

func (t *AdminModerationReport_ViewDetail_Subject) MarshalJSON() ([]byte, error) {
	if t.AdminRepo_View != nil {
		t.AdminRepo_View.LexiconTypeID = "com.atproto.admin.repo#view"
		return json.Marshal(t.AdminRepo_View)
	}
	if t.AdminRecord_View != nil {
		t.AdminRecord_View.LexiconTypeID = "com.atproto.admin.record#view"
		return json.Marshal(t.AdminRecord_View)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminModerationReport_ViewDetail_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.repo#view":
		t.AdminRepo_View = new(AdminRepo_View)
		return json.Unmarshal(b, t.AdminRepo_View)
	case "com.atproto.admin.record#view":
		t.AdminRecord_View = new(AdminRecord_View)
		return json.Unmarshal(b, t.AdminRecord_View)

	default:
		return nil
	}
}

type AdminModerationReport_View_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoStrongRef *RepoStrongRef
}

func (t *AdminModerationReport_View_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminModerationReport_View_Subject) UnmarshalJSON(b []byte) error {
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
