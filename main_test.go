package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryDelayUsesExponentialBackoffAndCapsAtInterval(t *testing.T) {
	interval := 5 * time.Minute
	want := []time.Duration{
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		80 * time.Second,
		160 * time.Second,
		5 * time.Minute,
		5 * time.Minute,
	}

	for index, expected := range want {
		failureCount := index + 1
		if got := retryDelay(interval, failureCount, 0.5); got != expected {
			t.Errorf("retryDelay(5m, %d) = %v, want %v", failureCount, got, expected)
		}
	}
}

func TestRetryDelayAddsBoundedJitter(t *testing.T) {
	if got := retryDelay(5*time.Minute, 1, 0); got != 8*time.Second {
		t.Errorf("minimum jitter delay = %v, want 8s", got)
	}
	if got := retryDelay(5*time.Minute, 1, 1); got != 12*time.Second {
		t.Errorf("maximum jitter delay = %v, want 12s", got)
	}
	if got := retryDelay(11*time.Second, 1, 1); got != 11*time.Second {
		t.Errorf("capped jitter delay = %v, want 11s", got)
	}
}

func TestRetryDelayNeverRetriesFasterThanShortInterval(t *testing.T) {
	interval := 5 * time.Second
	for failureCount := 1; failureCount <= 10; failureCount++ {
		if got := retryDelay(interval, failureCount, 0); got != interval {
			t.Errorf("retryDelay(5s, %d) = %v, want 5s", failureCount, got)
		}
	}
}

func TestRunContinuouslyRetriesAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	runContinuously(ctx, time.Millisecond, func() error {
		calls++
		if calls == 1 {
			return errors.New("temporary failure")
		}
		cancel()
		return nil
	})

	if calls != 2 {
		t.Errorf("sync calls = %d, want 2", calls)
	}
}
