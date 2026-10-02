package relay

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RussellLuo/slidingwindow"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/gorilla/websocket"
)

type retryAttempt struct {
	at     time.Time
	cursor string
}

// Pipe sockets keep handshake and stream failures inside synctest's fake clock.
func retryPipeDialer(t *testing.T, attempts chan<- retryAttempt, serve func(net.Conn, *http.Request, int)) *websocket.Dialer {
	t.Helper()
	n := 0
	return &websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			n++
			attempt := n
			go func() {
				defer server.Close()
				req, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					t.Errorf("read handshake: %v", err)
					return
				}
				attempts <- retryAttempt{at: time.Now(), cursor: req.URL.Query().Get("cursor")}
				serve(server, req, attempt)
			}()
			return client, nil
		},
	}
}

func retryUpgrade(conn net.Conn, req *http.Request, server string) error {
	hash := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, err := fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nServer: %s\r\n\r\n", base64.StdEncoding.EncodeToString(hash[:]), server)
	return err
}

func retrySubscription(t *testing.T) (*Slurper, *models.Host, *Subscription, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	host := &models.Host{Hostname: "retry.example", ID: 1, NoSSL: true}
	limit, _ := slidingwindow.NewLimiter(time.Second, 1000, windowFunc)
	sub := &Subscription{Hostname: host.Hostname, HostID: host.ID, ctx: ctx, cancel: cancel,
		Limiters: &StreamLimiters{PerSecond: limit, PerHour: limit, PerDay: limit}}
	config := DefaultSlurperConfig()
	config.ConcurrencyPerHost = 1
	config.PersistHostStatusCallback = func(context.Context, uint64, models.HostStatus) error {
		t.Error("unexpected host status change")
		return nil
	}
	s := &Slurper{Config: config, subs: map[string]*Subscription{host.Hostname: sub},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s, host, sub, cancel
}

func retryStart(s *Slurper, host *models.Host, sub *Subscription, dialer *websocket.Dialer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.subscribeWithDialer(sub.ctx, host, sub, dialer)
	}()
	return done
}

func TestSlurperNoProgressFailuresKeepCappedRetryPacing(t *testing.T) {
	for _, failedWork := range []time.Duration{0, 2 * time.Second} {
		t.Run(failedWork.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, host, sub, cancel := retrySubscription(t)
				attempts := make(chan retryAttempt)
				dialer := retryPipeDialer(t, attempts, func(conn net.Conn, req *http.Request, _ int) {
					if err := retryUpgrade(conn, req, "test-pds"); err != nil {
						t.Errorf("upgrade: %v", err)
						return
					}
					select {
					case <-time.After(failedWork):
					case <-sub.ctx.Done():
						return
					}
					_, _ = conn.Write([]byte{0x81, 1, 'x'}) // Invalid text frame.
				})
				start := time.Now()
				done := retryStart(s, host, sub, dialer)
				previous := <-attempts
				if previous.at != start {
					t.Errorf("initial dial delayed by %v", previous.at.Sub(start))
				}
				// More than 16 established failures must not mark a host offline.
				for b := 1; b <= 20; b++ {
					current := <-attempts
					maxDelay := 30 * time.Second
					if b < 6 {
						maxDelay = time.Second << (b - 1)
					}
					pause := current.at.Sub(previous.at) - failedWork
					if pause < maxDelay*4/5 || pause > maxDelay {
						t.Errorf("retry %d pause = %v, want [%v, %v]", b, pause, maxDelay*4/5, maxDelay)
					}
					previous = current
				}
				cancel()
				<-done
			})
		})
	}
}

func TestSlurperSlowFailedDialsKeepRetryPacingAndOfflinePolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, host, sub, cancel := retrySubscription(t)
		defer cancel()
		attempts := make(chan time.Time)
		statuses := make(chan models.HostStatus, 1)
		s.Config.PersistHostStatusCallback = func(_ context.Context, _ uint64, state models.HostStatus) error {
			statuses <- state
			return nil
		}
		dialer := &websocket.Dialer{NetDialContext: func(context.Context, string, string) (net.Conn, error) {
			attempts <- time.Now()
			time.Sleep(2 * time.Second)
			return nil, errors.New("slow failed dial")
		}}
		done := retryStart(s, host, sub, dialer)
		previous := <-attempts
		for b := 1; b < 16; b++ {
			current := <-attempts
			maxDelay := 30 * time.Second
			if b < 6 {
				maxDelay = time.Second << (b - 1)
			}
			pause := current.Sub(previous) - 2*time.Second
			if pause < maxDelay*4/5 || pause > maxDelay {
				t.Errorf("dial retry %d pause = %v, want [%v, %v]", b, pause, maxDelay*4/5, maxDelay)
			}
			previous = current
		}
		<-done
		if state := <-statuses; state != models.HostStatusOffline {
			t.Errorf("status = %v, want offline", state)
		}
	})
}

func TestSlurperRetryCancellationIsImmediate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, host, sub, cancel := retrySubscription(t)
		attempts := make(chan time.Time)
		dialer := &websocket.Dialer{NetDialContext: func(context.Context, string, string) (net.Conn, error) {
			attempts <- time.Now()
			return nil, errors.New("failed dial")
		}}
		done := retryStart(s, host, sub, dialer)
		<-attempts
		synctest.Wait()
		before := time.Now()
		cancel()
		<-done
		if elapsed := time.Since(before); elapsed != 0 {
			t.Errorf("cancellation waited %v for backoff", elapsed)
		}
	})
}

func TestSlurperCursorProgressReconnectsImmediatelyAndResetsRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, host, sub, cancel := retrySubscription(t)
		attempts := make(chan retryAttempt)
		persisted := make(chan int64, 1)
		s.Config.PersistCursorCallback = func(_ context.Context, cursors *[]HostCursor) error {
			persisted <- (*cursors)[0].LastSeq
			return nil
		}
		dialer := retryPipeDialer(t, attempts, func(conn net.Conn, req *http.Request, n int) {
			if err := retryUpgrade(conn, req, "test-pds"); err != nil {
				t.Errorf("upgrade: %v", err)
				return
			}
			if n == 3 {
				// Represent fresh processing progress without changing scheduler semantics.
				sub.LastSeq.Store(42)
			}
			_, _ = conn.Write([]byte{0x81, 1, 'x'})
		})
		done := retryStart(s, host, sub, dialer)
		<-attempts
		<-attempts
		third := <-attempts
		fourth := <-attempts
		if fourth.at != third.at || fourth.cursor != "42" {
			t.Errorf("progress reconnect = %+v after %+v, want immediate cursor 42", fourth, third)
		}
		if seq := <-persisted; seq != 42 {
			t.Errorf("persisted cursor = %d, want 42", seq)
		}
		fifth := <-attempts
		if pause := fifth.at.Sub(fourth.at); pause < 800*time.Millisecond || pause > time.Second {
			t.Errorf("retry after progress = %v, want [800ms, 1s]", pause)
		}
		cancel()
		<-done
	})
}

func TestSlurperBannedRelayClosesConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, host, sub, cancel := retrySubscription(t)
		defer cancel()
		attempts := make(chan retryAttempt)
		closed := make(chan error, 1)
		statuses := make(chan models.HostStatus, 1)
		s.Config.PersistHostStatusCallback = func(_ context.Context, _ uint64, state models.HostStatus) error {
			statuses <- state
			return nil
		}
		dialer := retryPipeDialer(t, attempts, func(conn net.Conn, req *http.Request, _ int) {
			if err := retryUpgrade(conn, req, "atproto-relay"); err != nil {
				closed <- err
				return
			}
			_, err := conn.Read(make([]byte, 1))
			closed <- err
		})
		done := retryStart(s, host, sub, dialer)
		<-attempts
		<-done
		if state := <-statuses; state != models.HostStatusBanned {
			t.Errorf("status = %v, want banned", state)
		}
		if err := <-closed; !errors.Is(err, io.EOF) {
			t.Errorf("banned socket read = %v, want EOF", err)
		}
	})
}
