package diskpersist

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/cmd/relay/stream"
)

func TestFlushReleasesJobsAndReusesBackingSlice(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	events := []*stream.XRPCStreamEvent{{Preserialized: []byte("first")}, {Preserialized: []byte("second")}}
	jobs := make([]persistJob, 2)
	for i, event := range events {
		buffer := bytes.NewBuffer(append([]byte(nil), event.Preserialized...))
		jobs[i] = persistJob{Bytes: buffer.Bytes(), Evt: event, Buffer: buffer}
	}
	backing := jobs
	var delivered []*stream.XRPCStreamEvent
	dp := &DiskPersistence{
		logfi:   file,
		outbuf:  bytes.NewBufferString("firstsecond"),
		evtbuf:  jobs,
		buffers: &sync.Pool{},
		broadcast: func(event *stream.XRPCStreamEvent) {
			delivered = append(delivered, event)
		},
	}
	if err := dp.flushLog(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || delivered[0] != events[0] || delivered[1] != events[1] {
		t.Fatalf("broadcast events = %v, want both original events in order", delivered)
	}
	if len(dp.evtbuf) != 0 || dp.outbuf.Len() != 0 {
		t.Fatal("flush did not reset queued output")
	}
	for _, job := range backing {
		if job.Bytes != nil || job.Evt != nil || job.Buffer != nil {
			t.Fatal("flushed queue backing slice retains event or buffer references")
		}
	}

	replacement := persistJob{Evt: &stream.XRPCStreamEvent{Preserialized: []byte("replacement")}}
	dp.evtbuf = append(dp.evtbuf, replacement)
	if &dp.evtbuf[0] != &backing[0] || backing[0].Evt != replacement.Evt {
		t.Fatal("new persistence job did not reuse the cleared backing slice")
	}
}
