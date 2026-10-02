//go:build darwin || linux

package diskpersist

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"syscall"
	"testing"

	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/stretchr/testify/require"
)

// Count handles to this particular file, independently of unrelated test descriptors.
func openPlaybackHandles(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	target := info.Sys().(*syscall.Stat_t)
	var limit syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit))
	count := 0
	for fd := 0; uint64(fd) < min(limit.Cur, 4096); fd++ {
		var stat syscall.Stat_t
		if syscall.Fstat(fd, &stat) == nil && stat.Dev == target.Dev && stat.Ino == target.Ino {
			count++
		}
	}
	return count
}

func TestPlaybackClosesFile(t *testing.T) {
	// A finalizer eventually closing leaked files must not hide an ownership bug.
	oldGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGC)
	for _, mode := range []string{"eof", "callback error", "decode error", "cursor error", "past end"} {
		t.Run(mode, func(t *testing.T) {
			path := playbackFixture(t, 2, 32)
			dp := &DiskPersistence{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			since := int64(0)
			if mode == "decode error" || mode == "cursor error" {
				require.NoError(t, os.WriteFile(path, []byte{1}, 0600))
			}
			if mode == "cursor error" {
				since = 1
			}
			if mode == "past end" {
				since = 10
			}
			require.Zero(t, openPlaybackHandles(t, path))
			_, err := dp.readEventsFrom(t.Context(), since, path, func(*stream.XRPCStreamEvent) error {
				if mode == "callback error" {
					return errors.New("stop")
				}
				return nil
			})
			if mode == "eof" || mode == "past end" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Zero(t, openPlaybackHandles(t, path), "replay must release its descriptor on every return path")
		})
	}
}
