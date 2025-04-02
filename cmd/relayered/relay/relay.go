package relay

import (
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/slurper"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/validator"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/eventmgr"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("relay")

type Relay struct {
	db        *gorm.DB
	dir       identity.Directory
	Logger    *slog.Logger
	Slurper   *slurper.Slurper
	Events    *eventmgr.EventManager
	Validator *validator.Validator
	Config    RelayConfig

	// extUserLk serializes a section of syncPDSAccount()
	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	// Account cache
	userCache *lru.Cache[string, *models.Account]
}

type RelayConfig struct {
	SSL                    bool
	DefaultRepoLimit       int64
	ConcurrencyPerPDS      int64
	MaxQueuePerPDS         int64
	ApplyPDSClientSettings func(c *xrpc.Client)
	SkipAccountHostCheck   bool // XXX: only used for testing
}

func DefaultRelayConfig() *RelayConfig {
	return &RelayConfig{
		SSL:               true,
		DefaultRepoLimit:  100,
		ConcurrencyPerPDS: 100,
		MaxQueuePerPDS:    1_000,
	}
}

func NewRelay(db *gorm.DB, vldtr *validator.Validator, evtman *eventmgr.EventManager, dir identity.Directory, config *RelayConfig) (*Relay, error) {

	if config == nil {
		config = DefaultRelayConfig()
	}

	uc, _ := lru.New[string, *models.Account](2_000_000)

	r := &Relay{
		db:        db,
		dir:       dir,
		Logger:    slog.Default().With("system", "relay"),
		Events:    evtman,
		Validator: vldtr,
		Config:    *config,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		userCache: uc,
	}

	if err := r.MigrateDatabase(); err != nil {
		return nil, err
	}

	slOpts := slurper.DefaultSlurperConfig()
	slOpts.SSL = config.SSL
	slOpts.DefaultRepoLimit = config.DefaultRepoLimit
	slOpts.ConcurrencyPerPDS = config.ConcurrencyPerPDS
	slOpts.MaxQueuePerPDS = config.MaxQueuePerPDS
	s, err := slurper.NewSlurper(db, r.handleFedEvent, slOpts, r.Logger)
	if err != nil {
		return nil, err
	}
	r.Slurper = s

	if err := r.Slurper.RestartAll(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Relay) MigrateDatabase() error {
	if err := r.db.AutoMigrate(models.DomainBan{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.PDS{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.Account{}); err != nil {
		return err
	}
	if err := r.db.AutoMigrate(models.AccountPreviousState{}); err != nil {
		return err
	}
	return nil
}

// simple check of connection to database
func (r *Relay) Healthcheck() error {
	return r.db.Exec("SELECT 1").Error
}
