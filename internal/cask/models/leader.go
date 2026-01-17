package models

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/bluesky-social/indigo/internal/cask/types"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	leaderKey = "leader"

	DefaultLeaseDuration       = 1 * time.Second
	DefaultRenewalInterval     = 300 * time.Millisecond
	DefaultAcquisitionInterval = 200 * time.Millisecond
)

var (
	ErrNotLeader    = errors.New("not the leader")
	ErrLeaseExpired = errors.New("lease expired")
)

func isLeaderExpired(leader *types.FirehoseLeader) bool {
	if leader == nil || leader.ExpiresAt == nil {
		return true
	}
	return time.Now().After(leader.ExpiresAt.AsTime())
}

type LeaderElectionConfig struct {
	Identity         string
	OnBecameLeader   func(ctx context.Context)
	OnLostLeadership func(ctx context.Context)
	Logger           *slog.Logger

	// Timing configuration (optional, defaults used if zero)
	LeaseDuration       time.Duration
	RenewalInterval     time.Duration
	AcquisitionInterval time.Duration
}

// LeaderElection coordinates leader election across multiple cask server processes
// using FoundationDB. Exactly one instance in the cluster is elected as the leader
// at any time, and is responsible for running the atproto firehose consumer. If the
// leader crashes or becomes unavailable, another instance will acquire the lease within
// ~1 second and take over firehose consumption, allowing for high availability without
// duplicate event processing.
type LeaderElection struct {
	models *Models
	db     *foundation.DB
	cfg    LeaderElectionConfig

	leaseDuration       time.Duration
	renewalInterval     time.Duration
	acquisitionInterval time.Duration

	stopped atomic.Bool
	stopCh  chan struct{}

	mu             sync.RWMutex
	isLeader       bool
	leaseExpiresAt time.Time
}

func (m *Models) NewLeaderElection(db *foundation.DB, cfg LeaderElectionConfig) *LeaderElection {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	le := &LeaderElection{
		models:              m,
		db:                  db,
		cfg:                 cfg,
		leaseDuration:       DefaultLeaseDuration,
		renewalInterval:     DefaultRenewalInterval,
		acquisitionInterval: DefaultAcquisitionInterval,
		stopCh:              make(chan struct{}),
	}

	if cfg.LeaseDuration > 0 {
		le.leaseDuration = cfg.LeaseDuration
	}
	if cfg.RenewalInterval > 0 {
		le.renewalInterval = cfg.RenewalInterval
	}
	if cfg.AcquisitionInterval > 0 {
		le.acquisitionInterval = cfg.AcquisitionInterval
	}

	return le
}

func (le *LeaderElection) IsLeader() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.isLeader && time.Now().Before(le.leaseExpiresAt)
}

func (le *LeaderElection) GetFirehoseLeader(ctx context.Context) (leader *types.FirehoseLeader, err error) {
	_, _, done := observe(ctx, le.db, "GetFirehoseLeader")
	defer func() { done(err) }()

	key := foundation.Pack(le.models.firehoseLeader, leaderKey)

	var ld types.FirehoseLeader
	err = foundation.ReadProto(le.db, &ld, func(tx fdb.ReadTransaction) ([]byte, error) {
		return tx.Get(key).Get()
	})
	if err != nil {
		if errors.Is(err, foundation.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	leader = &ld
	return
}

// TryAcquireLease attempts to acquire the leader lease. Returns true if this
// instance became the leader, false if another leader exists.
func (le *LeaderElection) TryAcquireLease(ctx context.Context) (acquired bool, err error) {
	_, _, done := observe(ctx, le.db, "TryAcquireLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.models.firehoseLeader, leaderKey)
	now := time.Now()

	acquired, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (bool, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return false, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data != nil {
			var current types.FirehoseLeader
			if err := proto.Unmarshal(data, &current); err != nil {
				return false, fmt.Errorf("failed to unmarshal leader info: %w", err)
			}
			if !isLeaderExpired(&current) && current.Id != le.cfg.Identity {
				return false, nil
			}
		}

		info := &types.FirehoseLeader{
			Id:         le.cfg.Identity,
			ExpiresAt:  timestamppb.New(now.Add(le.leaseDuration)),
			AcquiredAt: timestamppb.New(now),
			RenewedAt:  timestamppb.New(now),
		}

		infoData, err := proto.Marshal(info)
		if err != nil {
			return false, fmt.Errorf("failed to marshal leader info: %w", err)
		}

		tx.Set(key, infoData)
		return true, nil
	})
	if err != nil {
		return
	}

	if acquired {
		le.mu.Lock()
		le.leaseExpiresAt = now.Add(le.leaseDuration)
		wasLeader := le.isLeader
		le.isLeader = true
		le.mu.Unlock()

		if !wasLeader {
			le.cfg.Logger.Info("acquired leader lease", "identity", le.cfg.Identity)
			if le.cfg.OnBecameLeader != nil {
				le.cfg.OnBecameLeader(ctx)
			}
		}
	}

	return
}

func (le *LeaderElection) RenewLease(ctx context.Context) (err error) {
	_, _, done := observe(ctx, le.db, "RenewLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.models.firehoseLeader, leaderKey)
	now := time.Now()

	_, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (any, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return nil, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data == nil {
			return nil, ErrNotLeader
		}

		var current types.FirehoseLeader
		if err := proto.Unmarshal(data, &current); err != nil {
			return nil, fmt.Errorf("failed to unmarshal leader info: %w", err)
		}

		if current.Id != le.cfg.Identity {
			return nil, ErrNotLeader
		}

		if isLeaderExpired(&current) {
			return nil, ErrLeaseExpired
		}

		current.ExpiresAt = timestamppb.New(now.Add(le.leaseDuration))
		current.RenewedAt = timestamppb.New(now)

		infoData, err := proto.Marshal(&current)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal leader info: %w", err)
		}

		tx.Set(key, infoData)
		return nil, nil
	})
	if err == nil {
		le.mu.Lock()
		le.leaseExpiresAt = now.Add(le.leaseDuration)
		le.mu.Unlock()
	}

	return
}

func (le *LeaderElection) ReleaseLease(ctx context.Context) (err error) {
	_, _, done := observe(ctx, le.db, "ReleaseLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.models.firehoseLeader, leaderKey)

	_, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (any, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return nil, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data == nil {
			return nil, nil
		}

		var current types.FirehoseLeader
		if err := proto.Unmarshal(data, &current); err != nil {
			return nil, fmt.Errorf("failed to unmarshal leader info: %w", err)
		}

		if current.Id != le.cfg.Identity {
			return nil, nil
		}

		tx.Clear(key)
		return nil, nil
	})
	if err != nil {
		return
	}

	le.mu.Lock()
	wasLeader := le.isLeader
	le.isLeader = false
	le.mu.Unlock()

	if wasLeader {
		le.cfg.Logger.Info("released leader lease", "identity", le.cfg.Identity)
		if le.cfg.OnLostLeadership != nil {
			le.cfg.OnLostLeadership(ctx)
		}
	}

	return
}

// Run starts the leader election loop and blocks until Stop is called or the
// context is canceled.
func (le *LeaderElection) Run(ctx context.Context) error {
	le.cfg.Logger.Info("starting leader election", "identity", le.cfg.Identity)

	if _, err := le.TryAcquireLease(ctx); err != nil {
		le.cfg.Logger.Warn("initial lease acquisition failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			le.handleShutdown(context.Background())
			return ctx.Err()
		case <-le.stopCh:
			le.handleShutdown(ctx)
			return nil
		default:
		}

		le.mu.RLock()
		isLeader := le.isLeader
		le.mu.RUnlock()

		if isLeader {
			if err := le.RenewLease(ctx); err != nil {
				le.cfg.Logger.Warn("failed to renew lease", "error", err)
				le.mu.Lock()
				le.isLeader = false
				le.mu.Unlock()
				le.cfg.Logger.Info("lost leadership", "identity", le.cfg.Identity)
				if le.cfg.OnLostLeadership != nil {
					le.cfg.OnLostLeadership(ctx)
				}
			}

			select {
			case <-ctx.Done():
				le.handleShutdown(context.Background())
				return ctx.Err()
			case <-le.stopCh:
				le.handleShutdown(ctx)
				return nil
			case <-time.After(le.renewalInterval):
			}
		} else {
			if _, err := le.TryAcquireLease(ctx); err != nil {
				le.cfg.Logger.Warn("failed to acquire lease", "error", err)
			}

			select {
			case <-ctx.Done():
				le.handleShutdown(context.Background())
				return ctx.Err()
			case <-le.stopCh:
				le.handleShutdown(ctx)
				return nil
			case <-time.After(le.acquisitionInterval):
			}
		}
	}
}

func (le *LeaderElection) Stop() {
	if le.stopped.Load() {
		return
	}
	le.stopped.Store(true)
	close(le.stopCh)
}

func (le *LeaderElection) handleShutdown(ctx context.Context) {
	le.mu.RLock()
	isLeader := le.isLeader
	le.mu.RUnlock()

	if isLeader {
		le.cfg.Logger.Info("releasing lease on shutdown", "identity", le.cfg.Identity)
		if err := le.ReleaseLease(ctx); err != nil {
			le.cfg.Logger.Warn("failed to release lease on shutdown", "error", err)
		}
	}
}
