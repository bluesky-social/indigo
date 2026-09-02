package events

// Event kind labels, used as a bounded-cardinality metric dimension by the
// schedulers. These strings are part of the metrics contract: renaming one
// silently breaks dashboards and alerts that select on it.
const (
	EventKindCommit    = "commit"
	EventKindSync      = "sync"
	EventKindIdentity  = "identity"
	EventKindAccount   = "account"
	EventKindInfo      = "info"
	EventKindLabels    = "labels"
	EventKindLabelInfo = "label_info"
	EventKindError     = "error"
	EventKindUnknown   = "unknown"
)

// Kind returns a short, stable label describing which payload this event
// carries. An event with no recognized payload (or a nil receiver) reports
// EventKindUnknown rather than panicking, so that instrumentation can never
// take down a consumer.
//
// Exactly one kind is returned even if multiple payloads are somehow set;
// callers rely on that to keep paired gauge Inc/Dec on the same label set.
func (evt *XRPCStreamEvent) Kind() string {
	if evt == nil {
		return EventKindUnknown
	}
	switch {
	case evt.RepoCommit != nil:
		return EventKindCommit
	case evt.RepoSync != nil:
		return EventKindSync
	case evt.RepoIdentity != nil:
		return EventKindIdentity
	case evt.RepoAccount != nil:
		return EventKindAccount
	case evt.RepoInfo != nil:
		return EventKindInfo
	case evt.LabelLabels != nil:
		return EventKindLabels
	case evt.LabelInfo != nil:
		return EventKindLabelInfo
	case evt.Error != nil:
		return EventKindError
	default:
		return EventKindUnknown
	}
}
