package atproto

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.moderationAction

func init() {
}

type AdminModerationAction_Reversal struct {
	LexiconTypeID string `json:"$type,omitempty"`
	CreatedAt     string `json:"createdAt" cborgen:"createdAt"`
	CreatedBy     string `json:"createdBy" cborgen:"createdBy"`
	Reason        string `json:"reason" cborgen:"reason"`
}

type AdminModerationAction_View struct {
	LexiconTypeID     string                              `json:"$type,omitempty"`
	Action            *string                             `json:"action" cborgen:"action"`
	CreatedAt         string                              `json:"createdAt" cborgen:"createdAt"`
	CreatedBy         string                              `json:"createdBy" cborgen:"createdBy"`
	Id                int64                               `json:"id" cborgen:"id"`
	Reason            string                              `json:"reason" cborgen:"reason"`
	ResolvedReportIds []int64                             `json:"resolvedReportIds" cborgen:"resolvedReportIds"`
	Reversal          *AdminModerationAction_Reversal     `json:"reversal,omitempty" cborgen:"reversal"`
	Subject           *AdminModerationAction_View_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobCids   []string                            `json:"subjectBlobCids" cborgen:"subjectBlobCids"`
}

type AdminModerationAction_ViewCurrent struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Action        *string `json:"action" cborgen:"action"`
	Id            int64   `json:"id" cborgen:"id"`
}

type AdminModerationAction_ViewDetail struct {
	LexiconTypeID   string                                    `json:"$type,omitempty"`
	Action          *string                                   `json:"action" cborgen:"action"`
	CreatedAt       string                                    `json:"createdAt" cborgen:"createdAt"`
	CreatedBy       string                                    `json:"createdBy" cborgen:"createdBy"`
	Id              int64                                     `json:"id" cborgen:"id"`
	Reason          string                                    `json:"reason" cborgen:"reason"`
	ResolvedReports []*AdminModerationReport_View             `json:"resolvedReports" cborgen:"resolvedReports"`
	Reversal        *AdminModerationAction_Reversal           `json:"reversal,omitempty" cborgen:"reversal"`
	Subject         *AdminModerationAction_ViewDetail_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobs    []*AdminBlob_View                         `json:"subjectBlobs" cborgen:"subjectBlobs"`
}

type AdminModerationAction_ViewDetail_Subject struct {
	AdminRepo_View   *AdminRepo_View
	AdminRecord_View *AdminRecord_View
}

func (t *AdminModerationAction_ViewDetail_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminModerationAction_ViewDetail_Subject) UnmarshalJSON(b []byte) error {
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

type AdminModerationAction_View_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoStrongRef *RepoStrongRef
}

func (t *AdminModerationAction_View_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminModerationAction_View_Subject) UnmarshalJSON(b []byte) error {
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
