package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectedDiscoveryCancellationStopsAllWorkers(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		for _, cause := range []string{"request", "owner"} {
			t.Run(mode+"/"+cause, func(t *testing.T) {
				manager := bareManager()
				defer manager.Close()
				manager.config.IndexTimeoutMs = 5000
				manager.inspectFn = manager.inspectSource
				started, stopped := make(chan struct{}, 2), make(chan struct{}, 2)
				manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					started <- struct{}{}
					<-request.Context().Done()
					stopped <- struct{}{}
					return nil, request.Context().Err()
				})}
				source := selectedLease(mode)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := manager.Inspect(ctx, inspectRequest{Source: source})
					done <- err
				}()
				for range source.selectedTracks() {
					awaitSignal(t, started)
				}
				if cause == "owner" {
					manager.Close()
				} else {
					cancel()
				}
				select {
				case err := <-done:
					if asProblem(err).kind != "canceled" {
						t.Fatalf("cancellation=%v", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("discovery did not return after cancellation")
				}
				for range source.selectedTracks() {
					awaitSignal(t, stopped)
				}
				manager.mu.Lock()
				defer manager.mu.Unlock()
				if len(manager.bundles) != 0 || len(manager.inflight) != 0 {
					t.Fatal("canceled inspection remained cached/inflight")
				}
			})
		}
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("worker signal timed out")
	}
}

func TestDiscoveryFailureCancelsCompanionAndCanRetry(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	manager.config.IndexTimeoutMs = 5000
	manager.inspectFn = manager.inspectSource
	audioStarted, audioStopped := make(chan struct{}, 1), make(chan struct{}, 1)
	var recovered atomic.Bool
	manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if recovered.Load() {
			return fixtureResponse(request, indexedFixture(request.URL.Path == "/video")), nil
		}
		if request.URL.Path == "/audio" {
			audioStarted <- struct{}{}
			<-request.Context().Done()
			audioStopped <- struct{}{}
			return nil, request.Context().Err()
		}
		select {
		case <-audioStarted:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return response(http.StatusForbidden, ""), nil
	})}
	source := selectedLease("paired")
	_, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
	if asProblem(err).kind != "source_stale" {
		t.Fatalf("failure=%v", err)
	}
	awaitSignal(t, audioStopped)
	recovered.Store(true)
	if _, err := manager.Inspect(context.Background(), inspectRequest{Source: source}); err != nil {
		t.Fatalf("failed discovery was cached: %v", err)
	}
}

func TestConcurrentSelectedModesSingleflightIndependently(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	manager.config.IndexTimeoutMs = 5000
	var mu sync.Mutex
	calls := make(map[string]int)
	started, release := make(chan struct{}, 3), make(chan struct{})
	defer close(release)
	manager.inspectFn = func(ctx context.Context, source mediaLease) (*sourceBundle, error) {
		mu.Lock()
		calls[leaseKey(source)]++
		mu.Unlock()
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, contextProblem(ctx.Err())
		}
		return testBundle(source, strings.Repeat("a", 64)), nil
	}
	var wait sync.WaitGroup
	errors := make(chan error, 24)
	for _, mode := range []string{"paired", "video", "audio"} {
		for range 8 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, err := manager.Inspect(context.Background(), inspectRequest{Source: selectedLease(mode)})
				errors <- err
			}()
		}
	}
	for range 3 {
		awaitSignal(t, started)
	}
	// Release each mode's leader; later callers may reuse its completed bundle.
	for range 3 {
		release <- struct{}{}
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("lease groups=%v", calls)
	}
	for key, count := range calls {
		if count != 1 {
			t.Fatalf("lease %s discoveries=%d", key, count)
		}
	}
}

func TestSingleflightWaiterCancellationDoesNotCancelLeader(t *testing.T) {
	for _, mode := range []string{"video", "audio", "paired"} {
		t.Run(mode, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			manager.config.IndexTimeoutMs = 5000
			started, release := make(chan struct{}, 1), make(chan struct{})
			var calls atomic.Int32
			manager.inspectFn = func(ctx context.Context, source mediaLease) (*sourceBundle, error) {
				calls.Add(1)
				started <- struct{}{}
				select {
				case <-release:
					return testBundle(source, strings.Repeat("a", 64)), nil
				case <-ctx.Done():
					return nil, contextProblem(ctx.Err())
				}
			}
			source := selectedLease(mode)
			leader := make(chan error, 1)
			go func() {
				_, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
				leader <- err
			}()
			awaitSignal(t, started)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waiting := &waitingContext{Context: ctx, entered: make(chan struct{})}
			waiter := make(chan error, 1)
			go func() {
				_, err := manager.getBundle(waiting, source)
				waiter <- err
			}()
			awaitSignal(t, waiting.entered)
			cancel()
			select {
			case err := <-waiter:
				if asProblem(err).kind != "canceled" {
					t.Fatalf("waiter error=%v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("waiting caller did not cancel")
			}
			close(release)
			select {
			case err := <-leader:
				if err != nil || calls.Load() != 1 {
					t.Fatalf("leader=%v calls=%d", err, calls.Load())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("leader did not finish")
			}
		})
	}
}

type waitingContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *waitingContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func TestInvalidationPreservesReplacementBundle(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		manager := bareManager()
		source := selectedLease(mode)
		old := testBundle(source, "old")
		fresh := testBundle(source, "fresh")
		manager.bundles[leaseKey(source)] = fresh
		manager.invalidate(source, old)
		if manager.bundles[leaseKey(source)] != fresh {
			t.Fatalf("%s replacement evicted by old failure", mode)
		}
		manager.Close()
	}
}

func TestCanceledInspectionCannotUseCachedSelectedBundle(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		manager := bareManager()
		source := selectedLease(mode)
		manager.bundles[leaseKey(source)] = testBundle(source, strings.Repeat("a", 64))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := manager.Inspect(ctx, inspectRequest{Source: source}); asProblem(err).kind != "canceled" {
			t.Fatalf("%s cached inspection ignored cancellation: %v", mode, err)
		}
		manager.Close()
	}
}
