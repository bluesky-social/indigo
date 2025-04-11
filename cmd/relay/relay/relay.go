package relay

import (
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream/eventmgr"

	"github.com/RussellLuo/slidingwindow"
	"github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("relay")

type Relay struct {
	db          *gorm.DB
	Dir         identity.Directory
	Logger      *slog.Logger
	Slurper     *Slurper
	Events      *eventmgr.EventManager
	HostChecker HostChecker
	Config      RelayConfig

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	// Account cache
	accountCache *lru.Cache[string, *models.Account]

	HostPerDayLimiter *slidingwindow.Limiter
}

type RelayConfig struct {
	UserAgent             string
	DefaultRepoLimit      int64
	ConcurrencyPerHost    int
	QueueDepthPerHost     int
	LenientSyncValidation bool
	TrustedDomains        []string
	HostPerDayLimit       int64

	// If true, skip validation that messages for a given account (DID) are coming from the expected upstream host (PDS). Currently only used in tests; might be used for intermediate relays in the future.
	SkipAccountHostCheck bool
}

func DefaultRelayConfig() *RelayConfig {
	// NOTE: many of these defaults are clobbered by CLI arguments
	return &RelayConfig{
		UserAgent:          "indigo-relay",
		DefaultRepoLimit:   100,
		ConcurrencyPerHost: 40,
		QueueDepthPerHost:  1000,
		HostPerDayLimit:    50,
	}
}

func NewRelay(db *gorm.DB, evtman *eventmgr.EventManager, dir identity.Directory, config *RelayConfig) (*Relay, error) {

	if config == nil {
		config = DefaultRelayConfig()
	}

	uc, _ := lru.New[string, *models.Account](2_000_000)

	hc := NewHostClient(config.UserAgent)

	// NOTE: discarded second argument is not an `error` type

	r := &Relay{
		db:          db,
		Dir:         dir,
		Logger:      slog.Default().With("system", "relay"),
		Events:      evtman,
		HostChecker: hc,
		Config:      *config,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		accountCache: uc,

		HostPerDayLimiter: perDayLimiter(config.HostPerDayLimit),
	}

	if err := r.MigrateDatabase(); err != nil {
		return nil, err
	}

	slurpConfig := DefaultSlurperConfig()
	slurpConfig.DefaultRepoLimit = config.DefaultRepoLimit
	slurpConfig.ConcurrencyPerHost = config.ConcurrencyPerHost
	slurpConfig.QueueDepthPerHost = config.QueueDepthPerHost

	// register callbacks to persist cursors and host state in database
	slurpConfig.PersistCursorCallback = r.PersistHostCursors
	slurpConfig.PersistHostStatusCallback = r.UpdateHostStatus

	s, err := NewSlurper(r.processRepoEvent, slurpConfig, r.Logger)
	if err != nil {
		return nil, err
	}
	r.Slurper = s

	// TODO: should this happen in a separate "start" method, instead of "NewRelay()"?
	if err := r.ResubscribeAllHosts(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Relay) MigrateDatabase() error {
	if err := r.db.AutoMigrate(models.DomainBan{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.Host{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.Account{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.AccountRepo{}); err != nil {
		return err
	}
	return nil
}

// simple check of connection to database
func (r *Relay) Healthcheck() error {
	return r.db.Exec("SELECT 1").Error
}
