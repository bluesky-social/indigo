// NOTE: this is a hand-rolled implementation of this endpoint, until lexgen supports generating this endpoint
// see: https://github.com/bluesky-social/indigo/issues/10

package atproto

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

func init() {
	//util.RegisterType("app.bsky.admin.moderationReport", &AdminGetModerationReports_Report{})
}

// schema: com.atproto.admin.getModerationReports

type AdminGetModerationReports_Report struct {
	LexiconTypeID       string                             `json:"$type,omitempty"`
	Id                  int64                              `json:"id" cborgen:"id"`
	ReasonType          string                             `json:"reasonType" cborgen:"reasonType"`
	Reason              string                             `json:"reason" cborgen:"reason"`
	Subject             *AdminGetModerationReports_Subject `json:"subject" cborgen:"subject"`
	ReportedByDid       string                             `json:"reportedByDid" cborgen:"reportedByDid"`
	CreatedAt           string                             `json:"createdAt" cborgen:"createdAt"`
	ResolvedByActionIds []int64                            `json:"resolvedByActionIds" cborgen:"resolvedByActionIds"`
}

type AdminGetModerationReports_Subject struct {
	RepoRepoRef   *RepoRepoRef
	RepoStrongRef *RepoStrongRef
}

func (t *AdminGetModerationReports_Subject) MarshalJSON() ([]byte, error) {
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
func (t *AdminGetModerationReports_Subject) UnmarshalJSON(b []byte) error {
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
		return fmt.Errorf("closed enums must have a matching value")
	}
}

type AdminGetModerationReports_Output struct {
	LexiconTypeID string                              `json:"$type,omitempty"`
	Cursor        string                              `json:"cursor" cborgen:"cursor"`
	Reports       []*AdminGetModerationReports_Report `json:"reports" cborgen:"reports"`
}

func AdminGetModerationReports(ctx context.Context, c *xrpc.Client, subject *string, resolved *bool, before *string, limit *int64) (*AdminGetModerationReports_Output, error) {
	var out AdminGetModerationReports_Output

	params := map[string]interface{}{}
	if subject != nil {
		params["subject"] = *subject
	}
	if resolved != nil {
		params["resolved"] = resolved
	}
	if limit != nil {
		params["limit"] = *limit
	}
	if before != nil {
		params["before"] = *before
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getModerationReports", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
