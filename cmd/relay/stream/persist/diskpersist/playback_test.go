package diskpersist

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/stretchr/testify/require"
)

func playbackFixture(tb testing.TB, count, payload int) string {
	tb.Helper()
	var records bytes.Buffer
	for i := 1; i <= count; i++ {
		var body bytes.Buffer
		evt := &atproto.SyncSubscribeRepos_Sync{Did: "did:plc:test", Rev: "3m5k7r2a2bc22", Blocks: bytes.Repeat([]byte("x"), payload), Time: "2026-10-01T00:00:00Z", Seq: -1}
		require.NoError(tb, evt.MarshalCBOR(&body))
		var header [headerSize]byte
		binary.LittleEndian.PutUint32(header[4:], evtKindSync)
		binary.LittleEndian.PutUint32(header[8:], uint32(body.Len()))
		binary.LittleEndian.PutUint64(header[20:], uint64(i))
		records.Write(header[:])
		records.Write(body.Bytes())
	}
	path := filepath.Join(tb.TempDir(), "events")
	require.NoError(tb, os.WriteFile(path, records.Bytes(), 0600))
	return path
}

func TestPlaybackCancellation(t *testing.T) {
	path := playbackFixture(t, 3, 32)
	dp := &DiskPersistence{}
	t.Run("before open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := dp.readEventsFrom(ctx, 0, path, func(*stream.XRPCStreamEvent) error { t.Fatal("unexpected callback"); return nil })
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("between events", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		_, err := dp.readEventsFrom(ctx, 0, path, func(*stream.XRPCStreamEvent) error { calls++; cancel(); return nil })
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, calls)
	})
}

func TestPlaybackCursorAndCallback(t *testing.T) {
	path := playbackFixture(t, 3, 32)
	dp := &DiskPersistence{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var seqs []int64
	last, err := dp.readEventsFrom(t.Context(), 1, path, func(evt *stream.XRPCStreamEvent) error {
		seqs = append(seqs, evt.Sequence())
		require.Equal(t, bytes.Repeat([]byte("x"), 32), []byte(evt.RepoSync.Blocks))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, seqs)
	require.Equal(t, int64(3), *last)
	stop := errors.New("visitor stopped")
	_, err = dp.readEventsFrom(t.Context(), 0, path, func(*stream.XRPCStreamEvent) error { return stop })
	require.ErrorIs(t, err, stop)
}

func TestPlaybackHonorsRecordBoundary(t *testing.T) {
	path := playbackFixture(t, 2, 32)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Leave the complete CBOR in the file, but declare the first record one
	// byte shorter. Decoding must fail rather than read beyond its boundary.
	length := binary.LittleEndian.Uint32(data[8:])
	binary.LittleEndian.PutUint32(data[8:], length-1)
	require.NoError(t, os.WriteFile(path, data, 0600))
	dp := &DiskPersistence{}
	_, err = dp.readEventsFrom(t.Context(), 0, path, func(*stream.XRPCStreamEvent) error {
		t.Fatal("a truncated record must not be delivered")
		return nil
	})
	require.Error(t, err)
}

func BenchmarkPlaybackLog(b *testing.B) {
	for _, payload := range []int{5 * 1024, 100 * 1024} {
		b.Run(fmt.Sprintf("bytes%d", payload), func(b *testing.B) {
			const count = 128
			path := playbackFixture(b, count, payload)
			dp := &DiskPersistence{}
			ctx := context.Background()
			b.ReportAllocs()
			b.SetBytes(int64(count * payload))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				seen := 0
				_, err := dp.readEventsFrom(ctx, 0, path, func(*stream.XRPCStreamEvent) error { seen++; return nil })
				if err != nil || seen != count {
					b.Fatalf("read %d events: %v", seen, err)
				}
			}
		})
	}
}
