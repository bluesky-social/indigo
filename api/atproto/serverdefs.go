package atproto

// schema: com.atproto.server.defs

func init() {
}

type ServerDefs_InviteCode struct {
	Available  int64                       `json:"available" cborgen:"available"`
	Code       string                      `json:"code" cborgen:"code"`
	CreatedAt  string                      `json:"createdAt" cborgen:"createdAt"`
	CreatedBy  string                      `json:"createdBy" cborgen:"createdBy"`
	Disabled   bool                        `json:"disabled" cborgen:"disabled"`
	ForAccount string                      `json:"forAccount" cborgen:"forAccount"`
	Uses       []*ServerDefs_InviteCodeUse `json:"uses" cborgen:"uses"`
}

type ServerDefs_InviteCodeUse struct {
	UsedAt string `json:"usedAt" cborgen:"usedAt"`
	UsedBy string `json:"usedBy" cborgen:"usedBy"`
}
