package events

import (
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
)

func TestXRPCStreamEventKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		evt  *XRPCStreamEvent
		want string
	}{
		{"commit", &XRPCStreamEvent{RepoCommit: &comatproto.SyncSubscribeRepos_Commit{}}, EventKindCommit},
		{"sync", &XRPCStreamEvent{RepoSync: &comatproto.SyncSubscribeRepos_Sync{}}, EventKindSync},
		{"identity", &XRPCStreamEvent{RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{}}, EventKindIdentity},
		{"account", &XRPCStreamEvent{RepoAccount: &comatproto.SyncSubscribeRepos_Account{}}, EventKindAccount},
		{"info", &XRPCStreamEvent{RepoInfo: &comatproto.SyncSubscribeRepos_Info{}}, EventKindInfo},
		{"labels", &XRPCStreamEvent{LabelLabels: &comatproto.LabelSubscribeLabels_Labels{}}, EventKindLabels},
		{"label_info", &XRPCStreamEvent{LabelInfo: &comatproto.LabelSubscribeLabels_Info{}}, EventKindLabelInfo},
		{"error", &XRPCStreamEvent{Error: &ErrorFrame{}}, EventKindError},
		{"empty", &XRPCStreamEvent{}, EventKindUnknown},
		{"nil", nil, EventKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.evt.Kind(); got != tc.want {
				t.Fatalf("Kind() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Kind must be stable for a given event: the schedulers compute it once at
// enqueue and reuse it to decrement gauges at dequeue. A value that varied
// between calls would leak gauge counts.
func TestXRPCStreamEventKindIsStable(t *testing.T) {
	evt := &XRPCStreamEvent{RepoCommit: &comatproto.SyncSubscribeRepos_Commit{}}
	first := evt.Kind()
	for range 10 {
		if got := evt.Kind(); got != first {
			t.Fatalf("Kind() not stable: %q then %q", first, got)
		}
	}
}

// An event carrying more than one payload must still resolve to exactly one
// kind, so that gauge Inc/Dec pairs cannot straddle two label sets.
func TestXRPCStreamEventKindPrefersCommit(t *testing.T) {
	evt := &XRPCStreamEvent{
		RepoCommit:   &comatproto.SyncSubscribeRepos_Commit{},
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{},
	}
	if got := evt.Kind(); got != EventKindCommit {
		t.Fatalf("Kind() = %q, want %q", got, EventKindCommit)
	}
}
