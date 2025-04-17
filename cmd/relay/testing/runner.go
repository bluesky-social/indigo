package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"reflect"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/bluesky-social/indigo/cmd/relay/stream/eventmgr"
	"github.com/bluesky-social/indigo/cmd/relay/stream/persist/diskpersist"
	"github.com/bluesky-social/indigo/util/cliutil"
)

type SimpleRelay struct {
	Relay *relay.Relay
	Port  int
}

func (sr *SimpleRelay) handleSubscribeRepos(w http.ResponseWriter, r *http.Request) {
	err := sr.Relay.HandleSubscribeRepos(w, r, nil, "0.0.0.0")
	if err != nil {
		slog.Error("subscribeRepos", "err", err)
	}
}

func MustSimpleRelay(dir identity.Directory, tmpd string, lenient bool) *SimpleRelay {

	relayConfig := relay.DefaultRelayConfig()
	relayConfig.SkipAccountHostCheck = true
	relayConfig.LenientSyncValidation = lenient

	db, err := cliutil.SetupDatabase("sqlite://:memory:", 40)
	if err != nil {
		panic(err)
	}

	pOpts := diskpersist.DefaultDiskPersistOptions()
	persister, err := diskpersist.NewDiskPersistence(tmpd, "", db, pOpts)
	if err != nil {
		panic(err)
	}
	evtman := eventmgr.NewEventManager(persister)

	r, err := relay.NewRelay(db, evtman, dir, relayConfig)
	if err != nil {
		panic(err)
	}
	persister.SetUidSource(r)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	sr := SimpleRelay{
		Relay: r,
		Port:  port,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /xrpc/com.atproto.sync.subscribeRepos", sr.handleSubscribeRepos)

	slog.Info("starting test relay", "port", port)
	go func() {
		defer func() {
			_ = listener.Close()
		}()
		err := http.Serve(listener, mux)
		if err != nil {
			slog.Warn("test relay shutting down", "err", err)
		}
	}()
	return &sr
}

func LoadAndRunScenario(ctx context.Context, fpath string) error {

	s, err := LoadScenario(ctx, fpath)
	if err != nil {
		return err
	}
	return RunScenario(ctx, s)
}

func LoadScenario(ctx context.Context, fpath string) (*Scenario, error) {
	fixBytes, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}

	var s Scenario
	if err = json.Unmarshal(fixBytes, &s); err != nil {
		return nil, fmt.Errorf("parsing scenario JSON: %w", err)
	}

	for i := range s.Messages {
		e, err := s.Messages[i].Frame.XRPCStreamEvent()
		if err != nil {
			return nil, fmt.Errorf("parsing scenario XRPCStreamEvent: %w", err)
		}
		s.Messages[i].Frame.Event = e
	}

	return &s, nil
}

func RunScenario(ctx context.Context, s *Scenario) error {
	dir := identity.NewMockDirectory()
	for _, acc := range s.Accounts {
		dir.Insert(acc.Identity)
	}

	tmpd, err := os.MkdirTemp("", "relay-test-")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tmpd)
	}()

	p := NewProducer()
	hostPort := p.ListenRandom()
	defer p.Shutdown()

	sr := MustSimpleRelay(&dir, tmpd, s.Lenient)

	err = sr.Relay.SubscribeToHost(fmt.Sprintf("localhost:%d", hostPort), true, true)
	if err != nil {
		return err
	}

	c := NewConsumer(fmt.Sprintf("ws://localhost:%d", sr.Port))
	err = c.Connect(ctx, -1)
	if err != nil {
		return err
	}
	defer c.Shutdown()

	for i, msg := range s.Messages {
		slog.Info("sending test message", "index", i)
		c.Clear()
		evt, err := msg.Frame.XRPCStreamEvent()
		if err != nil {
			return fmt.Errorf("preparing XRPCStreamEvent: %w", err)
		}
		if err := p.Emit(evt); err != nil {
			return fmt.Errorf("failed sending test event: %w", err)
		}
		if !msg.Drop {
			evts, err := c.ConsumeEvents(1)
			if err != nil {
				return err
			}
			if len(evts) != 1 {
				return fmt.Errorf("consumed unexpected additional events: %d", len(evts))
			}
			if !EqualEvents(evt, evts[0]) {
				evt.RepoCommit.Blocks = nil
				evts[0].RepoCommit.Blocks = nil
				fmt.Printf("%+v\n", evt.RepoCommit)
				fmt.Printf("%+v\n", evts[0].RepoCommit)
				return fmt.Errorf("events didn't match")
			}
		} else {
			// TODO: verify nothing returned? especially if last message in set
		}
	}
	return nil
}

// checks if two XRPCStreamEvent are equal, ignoring sequence numbers and timestamps
func EqualEvents(a, b *stream.XRPCStreamEvent) bool {
	// TODO: this method is pretty manual, and should probably live next to the XRPCStreamEvent code
	if a.RepoCommit != nil {
		if b.RepoCommit == nil {
			return false
		}
		if a.RepoCommit.Repo != b.RepoCommit.Repo ||
			a.RepoCommit.Commit != b.RepoCommit.Commit ||
			!reflect.DeepEqual(a.RepoCommit.Blocks, b.RepoCommit.Blocks) ||
			!reflect.DeepEqual(a.RepoCommit.Blobs, b.RepoCommit.Blobs) ||
			!reflect.DeepEqual(a.RepoCommit.Ops, b.RepoCommit.Ops) ||
			!reflect.DeepEqual(a.RepoCommit.Since, b.RepoCommit.Since) ||
			!reflect.DeepEqual(a.RepoCommit.PrevData, b.RepoCommit.PrevData) ||
			a.RepoCommit.Rebase != b.RepoCommit.Rebase ||
			a.RepoCommit.Rev != b.RepoCommit.Rev ||
			a.RepoCommit.TooBig != b.RepoCommit.TooBig {
			return false
		}
		return true
	} else if a.RepoSync != nil {
		if b.RepoSync == nil {
			return false
		}
		if a.RepoSync.Did != b.RepoSync.Did ||
			!reflect.DeepEqual(a.RepoSync.Blocks, b.RepoSync.Blocks) ||
			a.RepoSync.Rev != b.RepoSync.Rev {
			return false
		}
		return true
	} else if a.RepoIdentity != nil {
		if b.RepoIdentity == nil {
			return false
		}
		if a.RepoIdentity.Did != b.RepoIdentity.Did ||
			!reflect.DeepEqual(a.RepoIdentity.Handle, b.RepoIdentity.Handle) {
			return false
		}
		return true
	} else if a.RepoAccount != nil {
		if b.RepoAccount == nil {
			return false
		}
		if a.RepoAccount.Did != b.RepoAccount.Did ||
			a.RepoAccount.Active != b.RepoAccount.Active ||
			!reflect.DeepEqual(a.RepoAccount.Status, b.RepoAccount.Status) {
			return false
		}
		return true
	}
	// NOTE: doesn't support all event types
	return false
}
