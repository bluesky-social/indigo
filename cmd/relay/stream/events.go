package stream

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	cbg "github.com/whyrusleeping/cbor-gen"
)

const (
	EvtKindErrorFrame = -1
	EvtKindMessage    = 1
)

type EventHeader struct {
	Op      int64  `json:"op" cborgen:"op"`
	MsgType string `json:"t,omitempty" cborgen:"t,omitempty"`
}

type XRPCStreamEvent struct {
	Error        *ErrorFrame
	RepoCommit   *comatproto.SyncSubscribeRepos_Commit
	RepoSync     *comatproto.SyncSubscribeRepos_Sync
	RepoIdentity *comatproto.SyncSubscribeRepos_Identity
	RepoInfo     *comatproto.SyncSubscribeRepos_Info
	RepoAccount  *comatproto.SyncSubscribeRepos_Account
	LabelLabels  *comatproto.LabelSubscribeLabels_Labels
	LabelInfo    *comatproto.LabelSubscribeLabels_Info

	// some private fields for internal routing perf
	PrivUid         uint64 `json:"-" cborgen:"-"`
	PrivPdsId       uint   `json:"-" cborgen:"-"`
	PrivRelevantPds []uint `json:"-" cborgen:"-"`
	Preserialized   []byte `json:"-" cborgen:"-"`
}

func (evt *XRPCStreamEvent) Serialize(wc io.Writer) error {
	header := EventHeader{Op: EvtKindMessage}
	var obj lexutil.CBOR

	switch {
	case evt.Error != nil:
		header.Op = EvtKindErrorFrame
		obj = evt.Error
	case evt.RepoCommit != nil:
		header.MsgType = "#commit"
		obj = evt.RepoCommit
	case evt.RepoSync != nil:
		header.MsgType = "#sync"
		obj = evt.RepoSync
	case evt.RepoIdentity != nil:
		header.MsgType = "#identity"
		obj = evt.RepoIdentity
	case evt.RepoAccount != nil:
		header.MsgType = "#account"
		obj = evt.RepoAccount
	case evt.RepoInfo != nil:
		header.MsgType = "#info"
		obj = evt.RepoInfo
	default:
		return fmt.Errorf("unrecognized event kind")
	}

	cborWriter := cbg.NewCborWriter(wc)
	if err := header.MarshalCBOR(cborWriter); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	return obj.MarshalCBOR(cborWriter)
}

func (xevt *XRPCStreamEvent) Deserialize(r io.Reader) error {
	var header EventHeader
	if err := header.UnmarshalCBOR(r); err != nil {
		return fmt.Errorf("reading header: %w", err)
	}
	switch header.Op {
	case EvtKindMessage:
		switch header.MsgType {
		case "#commit":
			var evt comatproto.SyncSubscribeRepos_Commit
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading repoCommit event: %w", err)
			}
			xevt.RepoCommit = &evt
		case "#sync":
			var evt comatproto.SyncSubscribeRepos_Sync
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading repoSync event: %w", err)
			}
			xevt.RepoSync = &evt
		case "#identity":
			var evt comatproto.SyncSubscribeRepos_Identity
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoIdentity = &evt
		case "#account":
			var evt comatproto.SyncSubscribeRepos_Account
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoAccount = &evt
		case "#info":
			// TODO: this might also be a LabelInfo (as opposed to RepoInfo)
			var evt comatproto.SyncSubscribeRepos_Info
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoInfo = &evt
		case "#labels":
			var evt comatproto.LabelSubscribeLabels_Labels
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading Labels event: %w", err)
			}
			xevt.LabelLabels = &evt
		}
	case EvtKindErrorFrame:
		var errframe ErrorFrame
		if err := errframe.UnmarshalCBOR(r); err != nil {
			return err
		}
		xevt.Error = &errframe
	default:
		return fmt.Errorf("unrecognized event stream type: %d", header.Op)
	}
	return nil
}

var ErrNoSeq = errors.New("event has no sequence number")

// serialize content into Preserialized cache
func (evt *XRPCStreamEvent) Preserialize() error {
	if evt.Preserialized != nil {
		return nil
	}
	var buf bytes.Buffer
	err := evt.Serialize(&buf)
	if err != nil {
		return err
	}
	evt.Preserialized = buf.Bytes()
	return nil
}

type ErrorFrame struct {
	Error   string `cborgen:"error"`
	Message string `cborgen:"message"`
}

func (evt *XRPCStreamEvent) Sequence() int64 {
	switch {
	case evt == nil:
		return -1
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq
	case evt.RepoSync != nil:
		return evt.RepoSync.Seq
	case evt.RepoIdentity != nil:
		return evt.RepoIdentity.Seq
	case evt.RepoAccount != nil:
		return evt.RepoAccount.Seq
	case evt.RepoInfo != nil:
		return -1
	case evt.Error != nil:
		return -1
	default:
		return -1
	}
}

func (evt *XRPCStreamEvent) GetSequence() (int64, bool) {
	switch {
	case evt == nil:
		return -1, false
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq, true
	case evt.RepoSync != nil:
		return evt.RepoSync.Seq, true
	case evt.RepoIdentity != nil:
		return evt.RepoIdentity.Seq, true
	case evt.RepoAccount != nil:
		return evt.RepoAccount.Seq, true
	case evt.RepoInfo != nil:
		return -1, false
	case evt.Error != nil:
		return -1, false
	default:
		return -1, false
	}
}
