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
	"github.com/bluesky-social/indigo/cmd/relayered/relay"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/validator"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/eventmgr"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/persist/diskpersist"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/labstack/echo/v4"
)

type SimpleRelay struct {
	Relay *relay.Relay
	Port  int
	echo  *echo.Echo
}

func MustSimpleRelay(dir identity.Directory, tmpd string) *SimpleRelay {

	relayConfig := relay.DefaultRelayConfig()
	relayConfig.SSL = false
	relayConfig.SkipAccountHostCheck = true

	db, err := cliutil.SetupDatabase("sqlite://:memory:", 40)
	if err != nil {
		panic(err)
	}

	pOpts := diskpersist.DefaultDiskPersistOptions()
	persister, err := diskpersist.NewDiskPersistence(tmpd, "", db, pOpts)
	if err != nil {
		panic(err)
	}
	vldtr := validator.NewValidator(dir)
	evtman := eventmgr.NewEventManager(persister)

	r, err := relay.NewRelay(db, vldtr, evtman, dir, relayConfig)
	if err != nil {
		panic(err)
	}
	persister.SetUidSource(r)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	slog.Info("starting test relay", "port", port)

	e := echo.New()
	e.HideBanner = true
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", r.EventsHandler)
	e.Listener = listener
	srv := &http.Server{}

	go func() {
		defer listener.Close()
		err := e.StartServer(srv)
		if err != nil {
			slog.Warn("test relay shutting down", "err", err)
		}
	}()
	return &SimpleRelay{
		Relay: r,
		Port:  port,
	}
}

func LoadAndRunScenario(ctx context.Context, fpath string) error {

	fixBytes, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}

	var s Scenario
	if err = json.Unmarshal(fixBytes, &s); err != nil {
		return err
	}

	dir := identity.NewMockDirectory()
	for _, acc := range s.Accounts {
		dir.Insert(acc.Identity)
	}

	tmpd, err := os.MkdirTemp("", "relayered-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	p := NewProducer()
	hostPort := p.ListenRandom()
	defer p.Shutdown()

	sr := MustSimpleRelay(&dir, tmpd)

	err = sr.Relay.Slurper.SubscribeToPds(ctx, fmt.Sprintf("localhost:%d", hostPort), true, true, nil)
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
			return err
		}
		p.Emit(evt)
		if !msg.Drop {
			evts, err := c.ConsumeEvents(1)
			if err != nil {
				return err
			}
			if len(evts) != 1 {
				return fmt.Errorf("consumed unexpected events")
			}
			if !EqualEvents(evt, evts[0]) {
				//evt.RepoCommit.Blocks = nil
				//evts[0].RepoCommit.Blocks = nil
				fmt.Printf("%+v\n", evt.RepoCommit)
				fmt.Printf("%+v\n", evts[0].RepoCommit)
				return fmt.Errorf("events didn't match")
			}
		} else {
			// TODO: verify nothing returned?
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
			a.RepoCommit.PrevData != b.RepoCommit.PrevData ||
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
