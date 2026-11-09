package engine

// Simplified variant of input parameters for com.atproto.moderation.createReport, for internal tracking
type ModReport struct {
	ReasonType string
	Comment    string
}

var (
	// Report reasons now use the new tools.ozone.report.defs#* scheme. The
	// legacy com.atproto.moderation.defs#* reasons are no longer emitted.
	// Values are remapped to their closest new-scheme equivalent (matching the
	// "Prefer new lexicon definition" hints in com.atproto.moderation.defs) so
	// existing rules transition without per-rule changes.
	ReportReasonSpam       = "tools.ozone.report.defs#reasonMisleadingSpam"
	ReportReasonViolation  = "tools.ozone.report.defs#reasonRuleOther"
	ReportReasonMisleading = "tools.ozone.report.defs#reasonMisleadingOther"
	ReportReasonSexual     = "tools.ozone.report.defs#reasonSexualUnlabeled"
	ReportReasonRude       = "tools.ozone.report.defs#reasonHarassmentOther"
	ReportReasonOther      = "tools.ozone.report.defs#reasonOther"
	// Child safety (CSAM/CSEM). Used by rules that specifically detect child
	// sexual abuse material (e.g. the abyss image-match rule) instead of the
	// generic ReportReasonViolation, which would otherwise land in
	// reasonRuleOther.
	ReportReasonChildSafetyCSAM = "tools.ozone.report.defs#reasonChildSafetyCSAM"
	// Other child-safety signals that do not establish a CSAM match.
	ReportReasonChildSafetyOther = "tools.ozone.report.defs#reasonChildSafetyOther"
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
	case ReportReasonChildSafetyCSAM:
		return "child-safety-csam"
	case ReportReasonChildSafetyOther:
		return "child-safety-other"
	default:
		return "unknown"
	}
}
