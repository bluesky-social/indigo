package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodicallyStopsOnTaskError(t *testing.T) {

	ctx := t.Context()

	var calls atomic.Int32
	taskErr := errors.New("boom")

	errCh := make(chan error, 1)
	go func() {
		errCh <- runPeriodically(ctx, 5*time.Millisecond, func(context.Context) error {
			calls.Add(1)
			return taskErr
		})
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, taskErr) {
			t.Fatalf("expected error to wrap task error, got %v", err)
		}
		if !strings.Contains(err.Error(), "periodic task failed") {
			t.Fatalf("expected wrapped error message, got %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("expected task to be called once, got %d", calls.Load())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runPeriodically did not return on task error")
	}
}

func TestRunPeriodicallyReturnsOnContextCancel(t *testing.T) {

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var calls atomic.Int32

	errCh := make(chan error, 1)
	go func() {
		errCh <- runPeriodically(ctx, 5*time.Millisecond, func(context.Context) error {
			if calls.Add(1) >= 2 {
				cancel()
			}
			return nil
		})
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		if calls.Load() < 2 {
			t.Fatalf("expected at least two task executions, got %d", calls.Load())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runPeriodically did not return on context cancel")
	}
}
