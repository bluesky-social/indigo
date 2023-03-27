package atproto

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.admin.defs

func init() {
}

type AdminDefs_ActionReversal struct {
	LexiconTypeID string `json:"$type,omitempty"`
	CreatedAt     string `json:"createdAt" cborgen:"createdAt"`
	CreatedBy     string `json:"createdBy" cborgen:"createdBy"`
	Reason        string `json:"reason" cborgen:"reason"`
}

type AdminDefs_ActionView struct {
	LexiconTypeID     string                        `json:"$type,omitempty"`
	Action            *string                       `json:"action" cborgen:"action"`
	CreatedAt         string                        `json:"createdAt" cborgen:"createdAt"`
	CreatedBy         string                        `json:"createdBy" cborgen:"createdBy"`
	Id                int64                         `json:"id" cborgen:"id"`
	Reason            string                        `json:"reason" cborgen:"reason"`
	ResolvedReportIds []int64                       `json:"resolvedReportIds" cborgen:"resolvedReportIds"`
	Reversal          *AdminDefs_ActionReversal     `json:"reversal,omitempty" cborgen:"reversal"`
	Subject           *AdminDefs_ActionView_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobCids   []string                      `json:"subjectBlobCids" cborgen:"subjectBlobCids"`
}

type AdminDefs_ActionViewCurrent struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Action        *string `json:"action" cborgen:"action"`
	Id            int64   `json:"id" cborgen:"id"`
}

type AdminDefs_ActionViewDetail struct {
	LexiconTypeID   string                              `json:"$type,omitempty"`
	Action          *string                             `json:"action" cborgen:"action"`
	CreatedAt       string                              `json:"createdAt" cborgen:"createdAt"`
	CreatedBy       string                              `json:"createdBy" cborgen:"createdBy"`
	Id              int64                               `json:"id" cborgen:"id"`
	Reason          string                              `json:"reason" cborgen:"reason"`
	ResolvedReports []*AdminDefs_ReportView             `json:"resolvedReports" cborgen:"resolvedReports"`
	Reversal        *AdminDefs_ActionReversal           `json:"reversal,omitempty" cborgen:"reversal"`
	Subject         *AdminDefs_ActionViewDetail_Subject `json:"subject" cborgen:"subject"`
	SubjectBlobs    []*AdminDefs_BlobView               `json:"subjectBlobs" cborgen:"subjectBlobs"`
}

type AdminDefs_ActionViewDetail_Subject struct {
	AdminDefs_RepoView   *AdminDefs_RepoView
	AdminDefs_RecordView *AdminDefs_RecordView
}

func (t *AdminDefs_ActionViewDetail_Subject) MarshalJSON() ([]byte, error) {
	if t.AdminDefs_RepoView != nil {
		t.AdminDefs_RepoView.LexiconTypeID = "com.atproto.admin.defs#repoView"
		return json.Marshal(t.AdminDefs_RepoView)
	}
	if t.AdminDefs_RecordView != nil {
		t.AdminDefs_RecordView.LexiconTypeID = "com.atproto.admin.defs#recordView"
		return json.Marshal(t.AdminDefs_RecordView)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminDefs_ActionViewDetail_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.defs#repoView":
		t.AdminDefs_RepoView = new(AdminDefs_RepoView)
		return json.Unmarshal(b, t.AdminDefs_RepoView)
	case "com.atproto.admin.defs#recordView":
		t.AdminDefs_RecordView = new(AdminDefs_RecordView)
		return json.Unmarshal(b, t.AdminDefs_RecordView)

	default:
		return nil
	}
}

type AdminDefs_ActionView_Subject struct {
	AdminDefs_RepoRef *AdminDefs_RepoRef
	RepoStrongRef     *RepoStrongRef
}

func (t *AdminDefs_ActionView_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminDefs_ActionView_Subject) UnmarshalJSON(b []byte) error {
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

type AdminDefs_BlobView struct {
	LexiconTypeID string                      `json:"$type,omitempty"`
	Cid           string                      `json:"cid" cborgen:"cid"`
	CreatedAt     string                      `json:"createdAt" cborgen:"createdAt"`
	Details       *AdminDefs_BlobView_Details `json:"details,omitempty" cborgen:"details"`
	MimeType      string                      `json:"mimeType" cborgen:"mimeType"`
	Moderation    *AdminDefs_Moderation       `json:"moderation,omitempty" cborgen:"moderation"`
	Size          int64                       `json:"size" cborgen:"size"`
}

type AdminDefs_BlobView_Details struct {
	AdminDefs_ImageDetails *AdminDefs_ImageDetails
	AdminDefs_VideoDetails *AdminDefs_VideoDetails
}

func (t *AdminDefs_BlobView_Details) MarshalJSON() ([]byte, error) {
	if t.AdminDefs_ImageDetails != nil {
		t.AdminDefs_ImageDetails.LexiconTypeID = "com.atproto.admin.defs#imageDetails"
		return json.Marshal(t.AdminDefs_ImageDetails)
	}
	if t.AdminDefs_VideoDetails != nil {
		t.AdminDefs_VideoDetails.LexiconTypeID = "com.atproto.admin.defs#videoDetails"
		return json.Marshal(t.AdminDefs_VideoDetails)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminDefs_BlobView_Details) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.defs#imageDetails":
		t.AdminDefs_ImageDetails = new(AdminDefs_ImageDetails)
		return json.Unmarshal(b, t.AdminDefs_ImageDetails)
	case "com.atproto.admin.defs#videoDetails":
		t.AdminDefs_VideoDetails = new(AdminDefs_VideoDetails)
		return json.Unmarshal(b, t.AdminDefs_VideoDetails)

	default:
		return nil
	}
}

type AdminDefs_ImageDetails struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Height        int64  `json:"height" cborgen:"height"`
	Width         int64  `json:"width" cborgen:"width"`
}

type AdminDefs_Moderation struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	CurrentAction *AdminDefs_ActionViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
}

type AdminDefs_ModerationDetail struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	Actions       []*AdminDefs_ActionView      `json:"actions" cborgen:"actions"`
	CurrentAction *AdminDefs_ActionViewCurrent `json:"currentAction,omitempty" cborgen:"currentAction"`
	Reports       []*AdminDefs_ReportView      `json:"reports" cborgen:"reports"`
}

type AdminDefs_RecordView struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	BlobCids      []string                `json:"blobCids" cborgen:"blobCids"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	IndexedAt     string                  `json:"indexedAt" cborgen:"indexedAt"`
	Moderation    *AdminDefs_Moderation   `json:"moderation" cborgen:"moderation"`
	Repo          *AdminDefs_RepoView     `json:"repo" cborgen:"repo"`
	Uri           string                  `json:"uri" cborgen:"uri"`
	Value         util.LexiconTypeDecoder `json:"value" cborgen:"value"`
}

type AdminDefs_RecordViewDetail struct {
	LexiconTypeID string                      `json:"$type,omitempty"`
	Blobs         []*AdminDefs_BlobView       `json:"blobs" cborgen:"blobs"`
	Cid           string                      `json:"cid" cborgen:"cid"`
	IndexedAt     string                      `json:"indexedAt" cborgen:"indexedAt"`
	Moderation    *AdminDefs_ModerationDetail `json:"moderation" cborgen:"moderation"`
	Repo          *AdminDefs_RepoView         `json:"repo" cborgen:"repo"`
	Uri           string                      `json:"uri" cborgen:"uri"`
	Value         util.LexiconTypeDecoder     `json:"value" cborgen:"value"`
}

type AdminDefs_RepoRef struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}

type AdminDefs_RepoView struct {
	LexiconTypeID  string                    `json:"$type,omitempty"`
	Did            string                    `json:"did" cborgen:"did"`
	Email          *string                   `json:"email,omitempty" cborgen:"email"`
	Handle         string                    `json:"handle" cborgen:"handle"`
	IndexedAt      string                    `json:"indexedAt" cborgen:"indexedAt"`
	Moderation     *AdminDefs_Moderation     `json:"moderation" cborgen:"moderation"`
	RelatedRecords []util.LexiconTypeDecoder `json:"relatedRecords" cborgen:"relatedRecords"`
}

type AdminDefs_RepoViewDetail struct {
	LexiconTypeID  string                      `json:"$type,omitempty"`
	Did            string                      `json:"did" cborgen:"did"`
	Email          *string                     `json:"email,omitempty" cborgen:"email"`
	Handle         string                      `json:"handle" cborgen:"handle"`
	IndexedAt      string                      `json:"indexedAt" cborgen:"indexedAt"`
	Moderation     *AdminDefs_ModerationDetail `json:"moderation" cborgen:"moderation"`
	RelatedRecords []util.LexiconTypeDecoder   `json:"relatedRecords" cborgen:"relatedRecords"`
}

type AdminDefs_ReportView struct {
	LexiconTypeID       string                        `json:"$type,omitempty"`
	CreatedAt           string                        `json:"createdAt" cborgen:"createdAt"`
	Id                  int64                         `json:"id" cborgen:"id"`
	Reason              *string                       `json:"reason,omitempty" cborgen:"reason"`
	ReasonType          *string                       `json:"reasonType" cborgen:"reasonType"`
	ReportedBy          string                        `json:"reportedBy" cborgen:"reportedBy"`
	ResolvedByActionIds []int64                       `json:"resolvedByActionIds" cborgen:"resolvedByActionIds"`
	Subject             *AdminDefs_ReportView_Subject `json:"subject" cborgen:"subject"`
}

type AdminDefs_ReportViewDetail struct {
	LexiconTypeID     string                              `json:"$type,omitempty"`
	CreatedAt         string                              `json:"createdAt" cborgen:"createdAt"`
	Id                int64                               `json:"id" cborgen:"id"`
	Reason            *string                             `json:"reason,omitempty" cborgen:"reason"`
	ReasonType        *string                             `json:"reasonType" cborgen:"reasonType"`
	ReportedBy        string                              `json:"reportedBy" cborgen:"reportedBy"`
	ResolvedByActions []*AdminDefs_ActionView             `json:"resolvedByActions" cborgen:"resolvedByActions"`
	Subject           *AdminDefs_ReportViewDetail_Subject `json:"subject" cborgen:"subject"`
}

type AdminDefs_ReportViewDetail_Subject struct {
	AdminDefs_RepoView   *AdminDefs_RepoView
	AdminDefs_RecordView *AdminDefs_RecordView
}

func (t *AdminDefs_ReportViewDetail_Subject) MarshalJSON() ([]byte, error) {
	if t.AdminDefs_RepoView != nil {
		t.AdminDefs_RepoView.LexiconTypeID = "com.atproto.admin.defs#repoView"
		return json.Marshal(t.AdminDefs_RepoView)
	}
	if t.AdminDefs_RecordView != nil {
		t.AdminDefs_RecordView.LexiconTypeID = "com.atproto.admin.defs#recordView"
		return json.Marshal(t.AdminDefs_RecordView)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *AdminDefs_ReportViewDetail_Subject) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.admin.defs#repoView":
		t.AdminDefs_RepoView = new(AdminDefs_RepoView)
		return json.Unmarshal(b, t.AdminDefs_RepoView)
	case "com.atproto.admin.defs#recordView":
		t.AdminDefs_RecordView = new(AdminDefs_RecordView)
		return json.Unmarshal(b, t.AdminDefs_RecordView)

	default:
		return nil
	}
}

type AdminDefs_ReportView_Subject struct {
	AdminDefs_RepoRef *AdminDefs_RepoRef
	RepoStrongRef     *RepoStrongRef
}

func (t *AdminDefs_ReportView_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminDefs_ReportView_Subject) UnmarshalJSON(b []byte) error {
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

type AdminDefs_VideoDetails struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Height        int64  `json:"height" cborgen:"height"`
	Length        int64  `json:"length" cborgen:"length"`
	Width         int64  `json:"width" cborgen:"width"`
}
