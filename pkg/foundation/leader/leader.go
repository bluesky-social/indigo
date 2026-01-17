package leader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
	"github.com/bluesky-social/indigo/pkg/clock"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/metrics"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"go.opentelemetry.io/otel/attribute"
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

type LeaderElectionConfig struct {
	ID               string
	OnBecameLeader   func(ctx context.Context)
	OnLostLeadership func(ctx context.Context)
	Logger           *slog.Logger

	// Timing configuration (optional, defaults used if zero)
	LeaseDuration       time.Duration
	RenewalInterval     time.Duration
	AcquisitionInterval time.Duration

	// Clock can be overridden with a mock clock in tests
	Clock clock.Clock
}

// LeaderElection coordinates leader election across multiple server processes
// using FoundationDB. Exactly one instance in the cluster is elected as the leader
// at any time. If the leader crashes or becomes unavailable, another instance will
// acquire the lease within ~1 second and take over, allowing for high availability
// without duplicate processing.
type LeaderElection struct {
	db    *foundation.DB
	dir   directory.DirectorySubspace
	cfg   LeaderElectionConfig
	clock clock.Clock

	leaseDuration       time.Duration
	renewalInterval     time.Duration
	acquisitionInterval time.Duration

	mu             sync.RWMutex
	cancel         context.CancelFunc
	isLeader       bool
	leaseExpiresAt time.Time
}

// New creates a new LeaderElection instance. The dirPath parameter specifies the
// FDB directory path where leader state will be stored.
func New(db *foundation.DB, dirPath []string, cfg LeaderElectionConfig) (*LeaderElection, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = &clock.RealClock{}
	}

	dir, err := directory.CreateOrOpen(db.Database, dirPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create leader election directory: %w", err)
	}

	le := &LeaderElection{
		db:                  db,
		dir:                 dir,
		cfg:                 cfg,
		clock:               cfg.Clock,
		leaseDuration:       DefaultLeaseDuration,
		renewalInterval:     DefaultRenewalInterval,
		acquisitionInterval: DefaultAcquisitionInterval,
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

	return le, nil
}

func (le *LeaderElection) IsLeader() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()

	return le.isLeader && le.clock.Now().Before(le.leaseExpiresAt)
}

// GetLeader returns the current leader info, or nil if no leader exists.
func (le *LeaderElection) GetLeader(ctx context.Context) (leader *prototypes.FirehoseLeader, err error) {
	_, span, done := foundation.Observe(ctx, le.db, "GetLeader")
	defer func() { done(err) }()

	key := foundation.Pack(le.dir, leaderKey)

	var ld prototypes.FirehoseLeader
	err = foundation.ReadProto(le.db, &ld, func(tx fdb.ReadTransaction) ([]byte, error) {
		return tx.Get(key).Get()
	})
	if err != nil {
		if errors.Is(err, foundation.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	span.SetAttributes(
		attribute.String("local_id", le.cfg.ID),
		attribute.String("leader_id", ld.Id),
		attribute.String("acquired_at", metrics.FormatPBTime(ld.AcquiredAt)),
		attribute.String("expires_at", metrics.FormatPBTime(ld.ExpiresAt)),
		attribute.String("renewed_at", metrics.FormatPBTime(ld.RenewedAt)),
	)

	leader = &ld
	return
}

// TryAcquireLease attempts to acquire the leader lease. Returns true if this
// instance became the leader, false if another leader exists.
func (le *LeaderElection) TryAcquireLease(ctx context.Context) (acquired bool, err error) {
	_, span, done := foundation.Observe(ctx, le.db, "TryAcquireLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.dir, leaderKey)
	now := le.clock.Now()

	acquired, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (bool, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return false, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data != nil {
			var existing prototypes.FirehoseLeader
			if err := proto.Unmarshal(data, &existing); err != nil {
				return false, fmt.Errorf("failed to unmarshal existing leader info: %w", err)
			}
			if !isLeaderExpired(&existing, now) && existing.Id != le.cfg.ID {
				// the leader's lease has not expired
				return false, nil
			}
		}

		// The leader does not exist, or the leader's lease has expired. Attempt to
		// claim the leader status.
		info := &prototypes.FirehoseLeader{
			Id:         le.cfg.ID,
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

	span.SetAttributes(
		attribute.String("local_id", le.cfg.ID),
		attribute.Bool("acquired", acquired),
	)

	if acquired {
		le.mu.Lock()
		le.leaseExpiresAt = now.Add(le.leaseDuration)
		wasLeader := le.isLeader
		le.isLeader = true
		le.mu.Unlock()

		if !wasLeader {
			le.cfg.Logger.Info("acquired new leader lease", "local_id", le.cfg.ID)
			if le.cfg.OnBecameLeader != nil {
				le.cfg.OnBecameLeader(ctx)
			}
		}
	}

	return
}

func (le *LeaderElection) RenewLease(ctx context.Context) (err error) {
	_, span, done := foundation.Observe(ctx, le.db, "RenewLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.dir, leaderKey)
	now := le.clock.Now()

	_, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (any, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return nil, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data == nil {
			// nobody has yet claimed the leader status, so do nothing
			return nil, ErrNotLeader
		}

		var current prototypes.FirehoseLeader
		if err := proto.Unmarshal(data, &current); err != nil {
			return nil, fmt.Errorf("failed to unmarshal leader info: %w", err)
		}

		if current.Id != le.cfg.ID {
			return nil, ErrNotLeader
		}

		if isLeaderExpired(&current, now) {
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

	span.SetAttributes(
		attribute.String("local_id", le.cfg.ID),
		attribute.String("expires_at", metrics.FormatTime(now.Add(le.leaseDuration))),
	)

	if err == nil {
		le.mu.Lock()
		le.leaseExpiresAt = now.Add(le.leaseDuration)
		le.mu.Unlock()
	}

	return
}

// Releases the lease if and only if this process is currently the leader
func (le *LeaderElection) ReleaseLease(ctx context.Context) (err error) {
	_, span, done := foundation.Observe(ctx, le.db, "ReleaseLease")
	defer func() { done(err) }()

	key := foundation.Pack(le.dir, leaderKey)

	var wasLeader bool
	wasLeader, err = foundation.Transaction(le.db, func(tx fdb.Transaction) (bool, error) {
		data, err := tx.Get(key).Get()
		if err != nil {
			return false, fmt.Errorf("failed to read leader info: %w", err)
		}

		if data == nil {
			return false, nil
		}

		var current prototypes.FirehoseLeader
		if err := proto.Unmarshal(data, &current); err != nil {
			return false, fmt.Errorf("failed to unmarshal leader info: %w", err)
		}

		if current.Id != le.cfg.ID {
			// we are not the leader, so do nothing
			return false, nil
		}

		tx.Clear(key)
		return true, nil
	})
	if err != nil {
		return
	}

	span.SetAttributes(
		attribute.String("local_id", le.cfg.ID),
		attribute.Bool("was_leader", wasLeader),
	)

	le.mu.Lock()
	le.isLeader = false
	le.mu.Unlock()

	if wasLeader {
		le.cfg.Logger.Info("released leader lease", "local_id", le.cfg.ID)
		if le.cfg.OnLostLeadership != nil {
			le.cfg.OnLostLeadership(ctx)
		}
	}

	return
}

// Run starts the leader election loop and blocks until Stop is called or the
// context is canceled.
func (le *LeaderElection) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	le.mu.Lock()
	le.cancel = cancel
	le.mu.Unlock()

	le.cfg.Logger.Info("starting leader election", "local_id", le.cfg.ID)

	if _, err := le.TryAcquireLease(ctx); err != nil {
		return fmt.Errorf("failed to try acquire initial lease: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			le.handleShutdown(context.Background())
			return ctx.Err()
		default:
		}

		le.mu.RLock()
		isLeader := le.isLeader
		le.mu.RUnlock()

		var waitFor time.Duration
		if isLeader {
			waitFor = le.renewalInterval
			if err := le.RenewLease(ctx); err != nil {
				le.cfg.Logger.Error("failed to renew lease", "error", err)
				le.mu.Lock()
				le.isLeader = false
				le.mu.Unlock()
				if le.cfg.OnLostLeadership != nil {
					le.cfg.OnLostLeadership(ctx)
				}
			}
		} else {
			waitFor = le.acquisitionInterval
			if _, err := le.TryAcquireLease(ctx); err != nil {
				le.cfg.Logger.Error("failed to acquire lease", "error", err)
			}
		}

		// wait for the appropriate period of time, or until shutdown was requested
		select {
		case <-ctx.Done():
			le.handleShutdown(context.Background())
			return ctx.Err()
		case <-le.clock.After(waitFor):
		}
	}
}

func (le *LeaderElection) Stop() {
	le.mu.Lock()
	defer le.mu.Unlock()

	if le.cancel != nil {
		le.cancel()
	}
}

func (le *LeaderElection) handleShutdown(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	le.mu.RLock()
	isLeader := le.isLeader
	le.mu.RUnlock()

	if !isLeader {
		return
	}

	le.cfg.Logger.Info("releasing lease on shutdown", "local_id", le.cfg.ID)
	if err := le.ReleaseLease(ctx); err != nil {
		le.cfg.Logger.Error("failed to release lease on shutdown", "error", err)
	}
}

func isLeaderExpired(leader *prototypes.FirehoseLeader, now time.Time) bool {
	if leader == nil || leader.ExpiresAt == nil {
		return true
	}
	return now.After(leader.ExpiresAt.AsTime())
}
