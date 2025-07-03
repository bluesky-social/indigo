package testing

import (
	"encoding/json"
	"fmt"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/identity"
	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/stream"
)

// represents an entire test case
type Scenario struct {
	Description string `json:"description"`
	Lenient     bool
	Accounts    []ScenarioAccount `json:"accounts"`
	Messages    []ScenarioMessage `json:"messages"`
}

type ScenarioAccount struct {
	Identity identity.Identity `json:"identity"`
	Status   string            `json:"status"`
}

type ScenarioMessage struct {
	Frame RepoEventFrame `json:"frame"`

	// whether relay  should drop message (instead of passing through)
	Drop bool `json:"drop"`

	// if the message is invalid (regardless of whether passed through
	Invalid bool `json:"invalid"`

	// whether account state / identity directory be updated
	Update bool `json:"update"`
}

// wrapper type appropriate for JSON encoding of firehose events
type RepoEventFrame struct {
	Header stream.EventHeader      `json:"header"`
	Body   json.RawMessage         `json:"body,omitempty"`
	Event  *stream.XRPCStreamEvent `json:"-"`
}

func (re *RepoEventFrame) XRPCStreamEvent() (*stream.XRPCStreamEvent, error) {
	if re.Event != nil {
		return re.Event, nil
	}
	if re.Header.Op == -1 {
		var evt stream.ErrorFrame
		if err := json.Unmarshal(re.Body, &evt); err != nil {
			return nil, err
		}
		return &stream.XRPCStreamEvent{Error: &evt}, nil
	} else if re.Header.Op != 1 {
		return nil, fmt.Errorf("unhandled header op: %d", re.Header.Op)
	}

	switch re.Header.MsgType {
	case "#commit":
		var evt comatproto.SyncSubscribeRepos_Commit
		if err := json.Unmarshal(re.Body, &evt); err != nil {
			return nil, err
		}
		return &stream.XRPCStreamEvent{RepoCommit: &evt}, nil
	case "#sync":
		var evt comatproto.SyncSubscribeRepos_Sync
		if err := json.Unmarshal(re.Body, &evt); err != nil {
			return nil, err
		}
		return &stream.XRPCStreamEvent{RepoSync: &evt}, nil
	case "#identity":
		var evt comatproto.SyncSubscribeRepos_Identity
		if err := json.Unmarshal(re.Body, &evt); err != nil {
			return nil, err
		}
		return &stream.XRPCStreamEvent{RepoIdentity: &evt}, nil
	case "#account":
		var evt comatproto.SyncSubscribeRepos_Account
		if err := json.Unmarshal(re.Body, &evt); err != nil {
			return nil, err
		}
		return &stream.XRPCStreamEvent{RepoAccount: &evt}, nil
	// TODO: add deprecated types, to test drop?
	default:
		return nil, fmt.Errorf("unhandled message type: %s", re.Header.MsgType)
	}
}
