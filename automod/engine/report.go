package engine

// Simplified variant of input parameters for com.atproto.moderation.createReport, for internal tracking
type ModReport struct {
	ReasonType string
	Comment    string
}

var (
	ReportReasonSpam       = "com.atproto.moderation.defs#reasonSpam"
	ReportReasonViolation  = "com.atproto.moderation.defs#reasonViolation"
	ReportReasonMisleading = "com.atproto.moderation.defs#reasonMisleading"
	ReportReasonSexual     = "com.atproto.moderation.defs#reasonSexual"
	ReportReasonRude       = "com.atproto.moderation.defs#reasonRude"
	ReportReasonOther      = "com.atproto.moderation.defs#reasonOther"
)

func ReasonShortName(reason string) string {
	switch reason {
	case ReportReasonSpam:
		return "spam"
	case ReportReasonViolation:
		return "violation"
	case ReportReasonMisleading:
		return "misleading"
	case ReportReasonSexual:
		return "sexual"
	case ReportReasonRude:
		return "rude"
	case ReportReasonOther:
		return "other"
	default:
		return "unknown"
	}
}
