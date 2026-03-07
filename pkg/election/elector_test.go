package election

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestElector creates an Elector backed by an in-process miniredis instance.
// TTL is short so tests run quickly without real-time waits.
func newTestElector(t *testing.T, mr *miniredis.Miniredis, nodeID string) *Elector {
	t.Helper()
	el, err := New(&ElectorConfig{
		RedisURL:      "redis://" + mr.Addr(),
		LockKey:       "test:leader",
		TTL:           300 * time.Millisecond,
		RetryInterval: 30 * time.Millisecond,
		NodeID:        nodeID,
	}, testLogger())
	if err != nil {
		t.Fatalf("failed to create elector for %s: %v", nodeID, err)
	}
	return el
}

// TestElector_AcquiresLockAndStartsProcessor verifies that a single node
// acquires the lock and starts the processor.
func TestElector_AcquiresLockAndStartsProcessor(t *testing.T) {
	mr := miniredis.RunT(t)
	el := newTestElector(t, mr, "node1")

	procStarted := make(chan struct{})
	runFn := func(ctx context.Context) error {
		close(procStarted)
		<-ctx.Done()
		return ctx.Err()
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- el.Run(appCtx, runFn) }()

	select {
	case <-procStarted:
	case <-time.After(time.Second):
		t.Fatal("processor did not start within timeout")
	}

	appCancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within timeout after cancel")
	}
}

// TestElector_OnlyOneLeaderWhenTwoNodesStart verifies that when two nodes race
// to acquire the lock simultaneously, exactly one wins — fixing the startup race
// that existed with the memberlist approach.
func TestElector_OnlyOneLeaderWhenTwoNodesStart(t *testing.T) {
	mr := miniredis.RunT(t)
	el1 := newTestElector(t, mr, "node1")
	el2 := newTestElector(t, mr, "node2")

	leaders := make(chan string, 2)
	runFn := func(id string) func(context.Context) error {
		return func(ctx context.Context) error {
			leaders <- id
			<-ctx.Done()
			return ctx.Err()
		}
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	go func() { _ = el1.Run(appCtx, runFn("node1")) }()
	go func() { _ = el2.Run(appCtx, runFn("node2")) }()

	// Exactly one leader must start within the window.
	select {
	case leader := <-leaders:
		t.Logf("leader acquired: %s", leader)
	case <-time.After(time.Second):
		t.Fatal("no leader elected within timeout")
	}

	// The second node must NOT also become leader at the same time.
	select {
	case second := <-leaders:
		t.Errorf("two leaders elected simultaneously: second=%s", second)
	case <-time.After(200 * time.Millisecond):
		// Good — only one leader running.
	}
}

// TestElector_FollowerTakesOverAfterLeaderShutdown verifies that when the
// current leader shuts down cleanly (releasing the lock), a waiting follower
// acquires the lock and starts the processor.
func TestElector_FollowerTakesOverAfterLeaderShutdown(t *testing.T) {
	mr := miniredis.RunT(t)
	el1 := newTestElector(t, mr, "node1")
	el2 := newTestElector(t, mr, "node2")

	// Start node1 as leader.
	node1Ctx, node1Cancel := context.WithCancel(context.Background())
	node1Started := make(chan struct{})
	node1Done := make(chan error, 1)
	go func() {
		node1Done <- el1.Run(node1Ctx, func(ctx context.Context) error {
			close(node1Started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	select {
	case <-node1Started:
	case <-time.After(time.Second):
		t.Fatal("node1 did not acquire leadership")
	}

	// Start node2 as follower (waiting for the lock).
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	node2Started := make(chan struct{})
	go func() {
		_ = el2.Run(appCtx, func(ctx context.Context) error {
			close(node2Started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	// Shut node1 down cleanly — it releases the lock immediately.
	node1Cancel()
	<-node1Done

	// node2 should acquire leadership quickly (within retryInterval).
	select {
	case <-node2Started:
		t.Log("node2 correctly took over after node1 shutdown")
	case <-time.After(2 * time.Second):
		t.Fatal("node2 did not acquire leadership after node1 shutdown")
	}
}

// TestElector_FollowerTakesOverAfterLockExpiry simulates a leader crash:
// the lock is not released but expires via TTL. The follower must take over
// once the lock expires, without the crashed leader doing anything.
func TestElector_FollowerTakesOverAfterLockExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	el1 := newTestElector(t, mr, "node1")
	el2 := newTestElector(t, mr, "node2")

	// node1 becomes leader.
	node1Ctx, node1Cancel := context.WithCancel(context.Background())
	defer node1Cancel()
	node1Started := make(chan struct{})
	go func() {
		_ = el1.Run(node1Ctx, func(ctx context.Context) error {
			close(node1Started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	select {
	case <-node1Started:
	case <-time.After(time.Second):
		t.Fatal("node1 did not acquire leadership")
	}

	// Fast-forward miniredis clock past the lock TTL (300ms).
	// This expires the key without node1 calling Release — simulating a crash.
	// node1's next heartbeat Refresh will fail, causing it to surrender
	// leadership internally; node2 can then acquire the lock.
	mr.FastForward(400 * time.Millisecond)

	// node2 should now acquire leadership.
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	node2Started := make(chan struct{})
	go func() {
		_ = el2.Run(appCtx, func(ctx context.Context) error {
			close(node2Started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	select {
	case <-node2Started:
		t.Log("node2 correctly took over after lock expiry (simulated crash)")
	case <-time.After(2 * time.Second):
		t.Fatal("node2 did not acquire leadership after lock expiry")
	}
}

// TestElector_PropagatesProcessorError verifies that a fatal error returned
// by runFn propagates through Run to the caller.
func TestElector_PropagatesProcessorError(t *testing.T) {
	mr := miniredis.RunT(t)
	el := newTestElector(t, mr, "node1")

	expectedErr := errors.New("database connection lost")
	runFn := func(_ context.Context) error {
		return expectedErr
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	runDone := make(chan error, 1)
	go func() { runDone <- el.Run(appCtx, runFn) }()

	select {
	case err := <-runDone:
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected wrapped %v, got %v", expectedErr, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not propagate processor error within timeout")
	}
}

// TestElector_GracefulShutdown verifies that cancelling appCtx stops the
// processor and Run returns nil (no error on clean shutdown).
func TestElector_GracefulShutdown(t *testing.T) {
	mr := miniredis.RunT(t)
	el := newTestElector(t, mr, "node1")

	procCtxCancelled := make(chan struct{})
	runFn := func(ctx context.Context) error {
		<-ctx.Done()
		close(procCtxCancelled)
		return ctx.Err()
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- el.Run(appCtx, runFn) }()

	// Wait for processor to start.
	time.Sleep(100 * time.Millisecond)

	appCancel()

	select {
	case <-procCtxCancelled:
	case <-time.After(time.Second):
		t.Fatal("proc context was not cancelled on shutdown")
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned unexpected error on graceful shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within timeout after graceful shutdown")
	}
}
