package election

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
)

// Elector manages leader election via a Redis distributed lock.
// Exactly one node in the cluster holds the lock at any time — that node
// is the leader and runs the processor. If the leader crashes, the lock
// expires after TTL and another node acquires it on its next poll.
type Elector struct {
	locker        *redislock.Client
	lockKey       string
	ttl           time.Duration
	retryInterval time.Duration
	nodeID        string
	logger        *slog.Logger
}

// ElectorConfig holds configuration for the Redis-based leader elector.
type ElectorConfig struct {
	// RedisURL is the Redis address. Accepts redis:// URLs or plain host:port.
	// Example: "redis.example.com:6379"
	RedisURL string

	// LockKey is the Redis key used as the distributed lock.
	// Should be scoped per environment to avoid cross-environment collisions.
	// Example: "eventsproc:sandbox:apac:leader"
	LockKey string

	// TTL is the lock lease duration. If the leader crashes without releasing
	// the lock, the lock expires after this duration and another node takes over.
	// Default: 15s
	TTL time.Duration

	// RetryInterval controls how often standby nodes poll for the lock.
	// Default: 5s
	RetryInterval time.Duration

	// NodeID is a unique identifier for this node, used in log messages.
	// Use the hostname.
	NodeID string
}

// New creates an Elector and verifies the Redis connection.
func New(cfg *ElectorConfig, logger *slog.Logger) (*Elector, error) {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		// Not a redis:// URL — treat as a plain host:port address.
		opt = &redis.Options{Addr: cfg.RedisURL}
	}

	rc := redis.NewClient(opt)

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rc.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed (%s): %w", cfg.RedisURL, err)
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 15 * time.Second
	}
	retryInterval := cfg.RetryInterval
	if retryInterval == 0 {
		retryInterval = 5 * time.Second
	}

	return &Elector{
		locker:        redislock.New(rc),
		lockKey:       cfg.LockKey,
		ttl:           ttl,
		retryInterval: retryInterval,
		nodeID:        cfg.NodeID,
		logger:        logger,
	}, nil
}

// Run manages the processor lifecycle based on Redis lock ownership.
// It blocks until appCtx is cancelled or runFn returns a fatal error.
//
// On each iteration:
//   - This node tries to acquire the Redis lock (single attempt, non-blocking).
//   - If acquired: run runFn under a child context with a heartbeat goroutine
//     that renews the lock every TTL/2. If the heartbeat fails (Redis
//     unreachable or lock expired), the child context is cancelled and runFn
//     stops gracefully.
//   - If not acquired: sleep retryInterval and retry.
func (e *Elector) Run(appCtx context.Context, runFn func(context.Context) error) error {
	for {
		lock, err := e.locker.Obtain(appCtx, e.lockKey, e.ttl, &redislock.Options{
			RetryStrategy: redislock.NoRetry(),
		})

		switch {
		case errors.Is(err, redislock.ErrNotObtained):
			e.logger.Debug("Waiting for leadership", "node", e.nodeID, "retry_in", e.retryInterval)
			select {
			case <-appCtx.Done():
				return nil
			case <-time.After(e.retryInterval):
			}
			continue

		case err != nil:
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			e.logger.Warn("Error obtaining lock", "node", e.nodeID, "error", err)
			select {
			case <-appCtx.Done():
				return nil
			case <-time.After(e.retryInterval):
			}
			continue
		}

		// Lock acquired — run as leader.
		e.logger.Info("Acquired leadership", "node", e.nodeID,
			"lock_key", e.lockKey, "ttl", e.ttl)

		if err := e.runAsLeader(appCtx, lock, runFn); err != nil {
			return err // fatal processor error — propagate to main
		}

		// runFn exited cleanly. Check if the whole app is shutting down.
		select {
		case <-appCtx.Done():
			return nil
		default:
			e.logger.Info("Re-entering follower mode", "node", e.nodeID)
		}
	}
}

// runAsLeader runs runFn under a child context while holding the lock.
// A heartbeat goroutine renews the lock every TTL/2.
// If the heartbeat fails (Redis error or lock expired/stolen), the leader
// context is cancelled and runFn stops gracefully.
// Blocks until both runFn and the heartbeat goroutine have exited.
func (e *Elector) runAsLeader(appCtx context.Context, lock *redislock.Lock, runFn func(context.Context) error) error {
	leaderCtx, leaderCancel := context.WithCancel(appCtx)
	defer leaderCancel()

	// Always attempt to release the lock on exit.
	// bsm/redislock uses a token-based Lua script for release, so it is safe
	// to call even if the lock has already expired or been acquired by another node.
	defer func() {
		if err := lock.Release(context.Background()); err != nil {
			e.logger.Debug("Lock release (may already be expired)", "node", e.nodeID, "error", err)
		} else {
			e.logger.Info("Released leadership lock", "node", e.nodeID)
		}
	}()

	// Heartbeat goroutine: renew the lock every TTL/2.
	// If renewal fails for a non-cancellation reason, surrender leadership by
	// cancelling the leader context (which stops runFn).
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(e.ttl / 2)
		defer ticker.Stop()
		for {
			select {
			case <-leaderCtx.Done():
				return
			case <-ticker.C:
				if err := lock.Refresh(leaderCtx, e.ttl, nil); err != nil {
					if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						e.logger.Warn("Lock refresh failed, surrendering leadership",
							"node", e.nodeID, "error", err)
						leaderCancel()
					}
					return
				}
				e.logger.Debug("Lock refreshed", "node", e.nodeID, "ttl", e.ttl)
			}
		}
	}()

	// Processor goroutine. Panic is converted to an error so it reaches the caller.
	procDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				procDone <- fmt.Errorf("processor panicked: %v", r)
			}
		}()
		procDone <- runFn(leaderCtx)
	}()

	// Wait for either the processor or leadership cancellation.
	var procErr error
	select {
	case procErr = <-procDone:
		leaderCancel() // stop heartbeat
	case <-leaderCtx.Done():
		// appCtx cancelled or heartbeat surrendered leadership.
		procErr = <-procDone // wait for runFn to exit
	}

	<-heartbeatDone // wait for heartbeat goroutine to finish before releasing lock

	if procErr != nil && !errors.Is(procErr, context.Canceled) {
		e.logger.Error("Processor exited with error", "node", e.nodeID, "error", procErr)
		return fmt.Errorf("processor error: %w", procErr)
	}
	e.logger.Info("Processor stopped", "node", e.nodeID)
	return nil
}
