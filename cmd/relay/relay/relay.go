package relay

import (
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream/eventmgr"

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

	// accountLk serializes a section of syncHostAccount()
	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	accountLk sync.Mutex

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	// Account cache
	accountCache *lru.Cache[string, *models.Account]
}

type RelayConfig struct {
	SSL                   bool
	DefaultRepoLimit      int64
	ConcurrencyPerHost    int
	QueueDepthPerHost     int
	SkipAccountHostCheck  bool // XXX: only used for testing
	LenientSyncValidation bool // XXX: wire through config

	// if true, ignore "requestCrawl"
	DisableNewHosts bool
	TrustedDomains  []string
}

func DefaultRelayConfig() *RelayConfig {
	// NOTE: many of these defaults are CLI arg defaults
	return &RelayConfig{
		SSL:                true,
		DefaultRepoLimit:   100,
		ConcurrencyPerHost: 40,
		QueueDepthPerHost:  1000,
	}
}

func NewRelay(db *gorm.DB, evtman *eventmgr.EventManager, dir identity.Directory, config *RelayConfig) (*Relay, error) {

	if config == nil {
		config = DefaultRelayConfig()
	}

	uc, _ := lru.New[string, *models.Account](2_000_000)

	hc := NewHostClient("relay") // TODO: pass-through a user-agent from config?

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
	}

	if err := r.MigrateDatabase(); err != nil {
		return nil, err
	}

	// XXX: need to pass-through more relay configs
	slurpConfig := DefaultSlurperConfig()
	slurpConfig.SSL = config.SSL
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
