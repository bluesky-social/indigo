package group_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/bluesky-social/indigo/internal/group"
)

type Group = group.G

func ExampleGroup_Wait() {
	// A Group's zero value is ready to use.
	var g group.G

	// Add a goroutine to the group.
	g.Add(func(c context.Context) error {
		select {
		case <-c.Done():
			return c.Err()
		case <-time.After(1 * time.Second):
			return errors.New("timed out")
		}
	})

	// Wait for all goroutines to finish.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output: timed out
}

func ExampleGroup_Wait_with_startup_error() {
	// A Group's zero value is ready to use.
	var g group.G

	// Add a goroutine to the group.
	g.Add(func(_ context.Context) error {
		return errors.New("startup error")
	})

	// Wait for all goroutines to finish, in this case it will return the startup error.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output: startup error
}

func ExampleGroup_Wait_with_panic() {
	// A Group's zero value is ready to use.
	var g group.G

	// Add a goroutine to the group.
	g.Add(func(c context.Context) error {
		panic("boom")
	})

	// Wait for all goroutines to finish.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output: panic: boom
}

func ExampleGroup_Wait_with_shutdown() {
	// A Group's zero value is ready to use.
	var g group.G

	shutdown := make(chan struct{})

	// Add a goroutine to the group.
	g.Add(func(c context.Context) error {
		select {
		case <-c.Done():
			return errors.New("stopped")
		case <-shutdown:
			return errors.New("shutdown")
		}
	})

	time.AfterFunc(100*time.Millisecond, func() {
		close(shutdown)
	})

	// Wait for all goroutines to finish.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output: shutdown
}

func ExampleGroup_Wait_with_context_cancel() {
	ctx := context.Background()
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(100*time.Millisecond))

	// pass WithContext option to New to use the provided context.
	g := group.New(group.WithContext(ctx))

	// Add a goroutine to the group.
	g.Add(func(c context.Context) error {
		select {
		case <-c.Done():
			return c.Err()
		}
	})

	// Cancel the context.
	cancel()

	// Wait for all goroutines to finish.
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output: context canceled
}

func ExampleGroup_Wait_with_signal() {
	ctx := context.Background()
	ctx, _ = signal.NotifyContext(ctx, os.Interrupt)

	g := group.New(group.WithContext(ctx))

	g.Add(MainHTTPServer)
	g.Add(DebugHTTPServer)
	g.Add(AsyncLogger)

	<-time.After(100 * time.Millisecond)

	// simulate ^C
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(os.Interrupt)

	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Unordered output:
	// async logger started
	// debug http server started
	// main http server started
	// async logger stopped
	// main http server stopped
	// debug http server stopped
	// context canceled
}

func ExampleGroup_Wait_with_http_shutdown() {
	ctx := context.Background()
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(100*time.Millisecond))
	defer cancel()

	g := group.New(group.WithContext(ctx))

	g.Add(func(ctx context.Context) error {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		svr := http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, "hello, world!")
			})}
		go func() {
			svr.Serve(l)
		}()

		<-ctx.Done() // wait for group to stop

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // five seconds graceful timeout
		defer cancel()
		return svr.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}

	// Output:
}
func MainHTTPServer(ctx context.Context) error {
	fmt.Println("main http server started")
	defer fmt.Println("main http server stopped")
	<-ctx.Done()
	return ctx.Err()
}

func DebugHTTPServer(ctx context.Context) error {
	fmt.Println("debug http server started")
	defer fmt.Println("debug http server stopped")
	<-ctx.Done()
	return ctx.Err()
}

func AsyncLogger(ctx context.Context) error {
	fmt.Println("async logger started")
	defer fmt.Println("async logger stopped")
	<-ctx.Done()
	return ctx.Err()
}
