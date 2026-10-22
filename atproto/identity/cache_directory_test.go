package identity

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type heldHandleDirectory struct {
	Directory
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *heldHandleDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*Identity, error) {
	ident, err := d.Directory.LookupHandle(ctx, handle)
	held := false
	d.once.Do(func() {
		held = true
		close(d.started)
	})
	if held {
		select {
		case <-d.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return ident, err
}

func TestPurgeFencesPendingHandleLookup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	handle := syntax.Handle("writer.example")
	oldDID, currentDID := syntax.DID("did:web:old.example"), syntax.DID("did:web:current.example")
	inner := NewMockDirectory()
	insert := func(did syntax.DID, host string) {
		inner.Insert(Identity{DID: did, Handle: handle, AlsoKnownAs: []string{"at://" + handle.String()}, Services: map[string]ServiceEndpoint{"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: host}}})
	}
	insert(oldDID, "https://old.example")
	held := &heldHandleDirectory{Directory: inner, started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(held.release) }) }
	defer unblock()
	cache := NewCacheDirectory(held, 32, time.Hour, time.Minute, time.Minute)
	oldDone := make(chan error, 1)
	go func() {
		_, err := cache.ResolveHandle(ctx, handle)
		oldDone <- err
	}()
	select {
	case <-held.started:
	case <-ctx.Done():
		t.Fatal("old handle resolution never started")
	}
	insert(currentDID, "https://current.example")
	atid, err := syntax.ParseAtIdentifier(handle.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Purge(ctx, atid); err != nil {
		t.Fatal(err)
	}
	resolved, err := cache.ResolveHandle(ctx, handle)
	if err != nil || resolved != currentDID {
		t.Fatalf("post-purge handle lookup joined old resolution: did=%s err=%v", resolved, err)
	}
	unblock()
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	ident, hit, err := cache.LookupHandleWithCacheState(ctx, handle)
	if err != nil || !hit || ident.DID != currentDID || ident.PDSEndpoint() != "https://current.example" {
		t.Fatalf("pre-purge handle result overwrote current cached identity: hit=%v err=%v", hit, err)
	}
}

type countedDIDDirectory struct {
	Directory
	calls atomic.Int32
}

func (d *countedDIDDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	d.calls.Add(1)
	return d.Directory.LookupDID(ctx, did)
}

func TestPurgePreservesUnrelatedResidentIdentity(t *testing.T) {
	ctx := context.Background()
	did := syntax.DID("did:web:unrelated.example")
	inner := NewMockDirectory()
	inner.Insert(Identity{DID: did, Handle: syntax.HandleInvalid})
	counted := &countedDIDDirectory{Directory: inner}
	cache := NewCacheDirectory(counted, 32, time.Hour, time.Minute, time.Minute)
	if _, err := cache.LookupDID(ctx, did); err != nil {
		t.Fatal(err)
	}
	atid, err := syntax.ParseAtIdentifier("did:web:migrating.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Purge(ctx, atid); err != nil {
		t.Fatal(err)
	}
	ident, hit, err := cache.LookupDIDWithCacheState(ctx, did)
	if err != nil || !hit || ident.DID != did || counted.calls.Load() != 1 {
		t.Fatalf("purge discarded an unrelated resident identity: hit=%v calls=%d err=%v", hit, counted.calls.Load(), err)
	}
}
