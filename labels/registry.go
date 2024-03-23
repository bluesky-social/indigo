package labels

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/RussellLuo/slidingwindow"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/util/labels"
	"github.com/gorilla/websocket"
	slogGorm "github.com/orandin/slog-gorm"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("label_registry")

var (
	// StatusConnected is the status of a label provider that has an active connection.
	StatusConnected = "connected"
	// StatusDisconnected is the status of a label provider that has no active connection.
	StatusDisconnected = "disconnected"
	// StatusErrored is the status of a label provider that has errored.
	StatusErrored = "errored"
	// StatusAttemptingToConnect is the status of a label provider that is attempting to connect.
	StatusAttemptingToConnect = "attempting_to_connect"
)

// LabelProvider is a label provider that hosts a label subscription websocket.
type LabelProvider struct {
	gorm.Model
	DID            string `gorm:"uniqueIndex"`
	Host           string
	Cursor         int64
	Status         string
	Error          string
	Banned         bool
	Autoconnect    bool
	LastActive     *time.Time
	LastConnected  *time.Time
	PerSecondLimit int64
	PerHourLimit   int64
	PerDayLimit    int64

	ConnectionAttempts int

	reg              *LabelRegistry `gorm:"-"`
	signalShutdown   chan struct{}  `gorm:"-"`
	shutdownComplete chan struct{}  `gorm:"-"`
}

// LabelRegistry is a registry of label providers
type LabelRegistry struct {
	db        *gorm.DB
	provLk    sync.RWMutex
	Providers []*LabelProvider
	dir       *identity.CacheDirectory

	LimitMux sync.RWMutex
	Limiters map[uint]*Limiters

	logger         *slog.Logger
	signalShutdown chan struct{}
}

// NewLabelRegistry returns a new label registry connected to the given SQLite database.
func NewLabelRegistry(
	ctx context.Context,
	sqlitePath string,
	logger *slog.Logger,
	dir *identity.CacheDirectory,
) (*LabelRegistry, error) {
	gormLogger := slogGorm.New(slogGorm.WithLogger(logger))
	db, err := gorm.Open(sqlite.Open(sqlitePath), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return nil, fmt.Errorf("failed to set journal_mode=WAL: %w", err)
	}

	if err := db.Exec("PRAGMA synchronous=normal;").Error; err != nil {
		return nil, fmt.Errorf("failed to set synchronous=normal: %w", err)
	}

	if err := db.AutoMigrate(&LabelProvider{}); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate label providers: %w", err)
	}

	rawDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw DB from gorm: %w", err)
	}

	// Set the maximum number of open connections to 1 for sqlite
	rawDB.SetMaxOpenConns(1)

	var providers []*LabelProvider
	if err := db.Find(&providers).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("failed to load label providers from DB: %w", err)
		}
	}

	reg := LabelRegistry{
		db:             db,
		dir:            dir,
		Providers:      providers,
		logger:         logger,
		signalShutdown: make(chan struct{}),
		Limiters:       make(map[uint]*Limiters),
	}

	for _, provider := range providers {
		provider.reg = &reg
		provider.signalShutdown = make(chan struct{})
		provider.shutdownComplete = make(chan struct{})
	}

	return &reg, nil
}

// Start starts the label registry and all connected providers.
func (r *LabelRegistry) Start(ctx context.Context) {
	r.logger.Info("starting label registry")

	go func() {
		for _, provider := range r.Providers {
			if provider.Autoconnect {
				go r.ConnectProvider(provider)
			}
		}
		<-ctx.Done()
		r.Shutdown()
	}()
}

// Shutdown shuts down the label registry and all connected providers.
func (r *LabelRegistry) Shutdown() {
	r.logger.Info("shutting down label registry")
	close(r.signalShutdown)
	for _, provider := range r.Providers {
		<-provider.shutdownComplete
		r.logger.Info("label provider shutdown complete", "provider", provider.DID)
	}
	r.logger.Info("label registry shutdown complete")
}

// GetProviderByDID returns a label provider by DID
func (r *LabelRegistry) GetProviderByDID(did string) *LabelProvider {
	// Load the provider from the DB
	provider := &LabelProvider{}
	if err := r.db.Where("d_id = ?", did).First(provider).Error; err != nil {
		return nil
	}

	return provider
}

// GetProviderByHost returns a label provider by host
func (r *LabelRegistry) GetProviderByHost(host string) *LabelProvider {
	// Load the provider from the DB
	provider := &LabelProvider{}
	if err := r.db.Where("host = ?", host).First(provider).Error; err != nil {
		return nil
	}

	return provider
}

// GetHostAndKeyByDID returns the host and key for a label provider by DID
func (r *LabelRegistry) GetHostAndKeyByDID(ctx context.Context, did syntax.DID) (string, crypto.PublicKey, error) {
	ctx, span := tracer.Start(ctx, "GetHostAndKeyByDID")
	defer span.End()

	span.SetAttributes(attribute.String("did", did.String()))

	id, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		return "", nil, fmt.Errorf("failed to lookup labeler DID: %w", err)
	}

	endpoint := ""
	// Ensure we have a labeler service in the DID doc.
	for id, svc := range id.Services {
		if strings.HasSuffix(id, "atproto_labeler") && svc.Type == "AtprotoLabeler" {
			endpoint = svc.URL
			break
		}
	}

	// Extract the labeler host.
	if endpoint == "" {
		return "", nil, fmt.Errorf("labeler DID has no Labeler service in did doc: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse labeler service URL: %w", err)
	}
	host := u.Hostname()

	// Extract the labeler signing public key.
	key, err := id.GetPublicKey("atproto_label")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get public key for labeler: %w", err)
	}

	return host, key, nil
}

var (
	// DefaultPerSecondLimit is the default rate limit for label providers per second.
	DefaultPerSecondLimit = int64(5)
	// DefaultPerHourLimit is the default rate limit for label providers per hour.
	DefaultPerHourLimit = int64(5_000)
	// DefaultPerDayLimit is the default rate limit for label providers per day.
	DefaultPerDayLimit = int64(50_000)
)

// RegisterProvider registers a new label provider
func (r *LabelRegistry) RegisterProvider(did, host string) (*LabelProvider, error) {
	provider := &LabelProvider{
		DID:              did,
		Host:             host,
		Status:           StatusDisconnected,
		Autoconnect:      true,
		PerSecondLimit:   DefaultPerSecondLimit,
		PerHourLimit:     DefaultPerHourLimit,
		PerDayLimit:      DefaultPerDayLimit,
		signalShutdown:   make(chan struct{}),
		shutdownComplete: make(chan struct{}),
	}

	if err := r.db.Create(provider).Error; err != nil {
		return nil, err
	}

	r.provLk.Lock()
	r.Providers = append(r.Providers, provider)
	r.provLk.Unlock()

	return provider, nil
}

var supportedLabelVersions = []int64{1}

// GetOrRegisterProvider returns a label provider by DID, or registers a new one if it doesn't exist
func (r *LabelRegistry) GetOrRegisterProvider(did, host string) (*LabelProvider, bool, error) {
	provider := r.GetProviderByDID(did)
	if provider != nil {
		return provider, false, nil
	}

	provider, err := r.RegisterProvider(did, host)
	if err != nil {
		return nil, false, err
	}

	return provider, true, nil
}

// ConnectProvider connects to a label provider
func (r *LabelRegistry) ConnectProvider(provider *LabelProvider) error {
	log := r.logger.With("provider", provider.DID)
	ctx, cancel := context.WithCancel(context.Background())

	if provider.Banned {
		log.Info("skipping connection attempt for banned label provider")
		cancel()
		return nil
	}

	// Reinitialize the shutdown channels
	provider.signalShutdown = make(chan struct{})
	provider.shutdownComplete = make(chan struct{})

	errChan := make(chan error)

	go func() {
		var providerExitStatus string
		var providerExitError string
		select {
		case <-r.signalShutdown:
			log.Info("shutting down label provider connection on signal from registry", "provider", provider.DID)
			providerExitStatus = StatusDisconnected
			providerExitError = ""
		case err := <-errChan:
			log.Info("shutting down label provider connection on error", "provider", provider.DID, "err", err)
			providerExitStatus = StatusErrored
			providerExitError = err.Error()
		case <-provider.signalShutdown:
			log.Info("shutting down label provider connection on signal from provider", "provider", provider.DID)
			providerExitStatus = StatusDisconnected
			providerExitError = ""
		}
		cancel()

		// Update the provider exit status and error on shutdown.
		err := r.db.Model(&LabelProvider{}).Where("id = ?", provider.ID).Updates(LabelProvider{Status: providerExitStatus, Error: providerExitError}).Error
		if err != nil {
			log.Error("failed to update provider exit status", "err", err)
		}
		close(provider.shutdownComplete)
	}()

	providerDID := provider.DID
	providerUID := provider.ID

	rsc := events.RepoStreamCallbacks{
		LabelLabels: func(evt *comatproto.LabelSubscribeLabels_Labels) error {
			ctx, span := tracer.Start(ctx, "LabelEventReceived")
			defer span.End()

			log := log.With("seq", evt.Seq)

			span.SetAttributes(
				attribute.String("provider", providerDID),
				attribute.Int64("seq", evt.Seq),
				attribute.Int("label_count", len(evt.Labels)),
			)

			if len(evt.Labels) == 0 {
				return nil
			}

			lastMessageAt := time.Now()

			_, key, err := r.GetHostAndKeyByDID(ctx, syntax.DID(providerDID))
			if err != nil {
				log.Error("failed to get host and key by DID", "err", err)
				span.RecordError(fmt.Errorf("failed to get host and key by DID: %w", err))
				errChan <- fmt.Errorf("failed to get host and key by DID: %w", err)
				return nil
			}

			for _, label := range evt.Labels {
				if label.Ver == nil {
					log.Warn("label missing version, skipping", "label", label)
					continue
				}
				if !slices.Contains(supportedLabelVersions, *label.Ver) {
					log.Warn("unsupported label version, skipping", "version", *label.Ver)
					continue
				}

				unsignedLabel := labels.UnsignedLabel{
					Cid: label.Cid,
					Cts: label.Cts,
					Exp: label.Exp,
					Neg: label.Neg,
					Src: label.Src,
					Uri: label.Uri,
					Val: label.Val,
					Ver: label.Ver,
				}

				bytesToSign, err := unsignedLabel.BytesForSigning()
				if err != nil {
					log.Error("failed to get bytes for signing", "err", err)
					span.RecordError(fmt.Errorf("failed to get bytes for signing: %w", err))
					errChan <- fmt.Errorf("failed to get bytes for signing: %w", err)
					return nil
				}

				if label.Sig != nil {
					err = key.HashAndVerify(bytesToSign, label.Sig)
					if err != nil {
						// Ignore labels with bad signatures but log them
						log.Error("failed to verify label signature", "err", err, "label", label, "bytes_to_sign", bytesToSign)
						span.RecordError(fmt.Errorf("failed to verify label signature: %w", err))
						continue
					}
				}

				labelsReceivedCounter.WithLabelValues(providerDID).Inc()

				// Do something with the label
				log.Info("received label", "label", label)
			}

			// Update the the provider in the DB with the new cursor
			err = r.db.Model(&LabelProvider{}).Where("id = ?", providerUID).Updates(LabelProvider{Cursor: evt.Seq, LastActive: &lastMessageAt}).Error
			if err != nil {
				log.Error("failed to update provider cursor", "err", err)
				span.RecordError(fmt.Errorf("failed to update provider cursor: %w", err))
				errChan <- fmt.Errorf("failed to update provider cursor: %w", err)
				return nil
			}

			return nil
		},
	}

	limits := r.GetOrCreateLimiters(providerUID, provider.PerSecondLimit, provider.PerHourLimit, provider.PerDayLimit)
	limiters := []*slidingwindow.Limiter{limits.PerSecond, limits.PerHour, limits.PerDay}

	instrumentedRSC := events.NewInstrumentedRepoStreamCallbacks(limiters, rsc.EventHandler)

	scheduler := sequential.NewScheduler(providerDID, instrumentedRSC.EventHandler)

	var backoff int
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down label provider connection loop", "provider", providerDID)
			return nil
		default:
		}

		// Load the provider from the DB
		provider := &LabelProvider{}
		if err := r.db.First(provider, providerUID).Error; err != nil {
			log.Error("failed to load provider from DB", "err", err)
			errChan <- fmt.Errorf("failed to load provider from DB: %w", err)
			return nil
		}

		// Check if the provider is banned
		if provider.Banned {
			log.Info("skipping connection attempt for banned label provider", "provider", provider.DID)
			errChan <- fmt.Errorf("skipping connection attempt for banned label provider: %s", provider.DID)
			return nil
		}

		// Fetch the Host before attempting to connect (to catch identity changes)
		host, _, err := r.GetHostAndKeyByDID(ctx, syntax.DID(providerDID))
		if err != nil {
			log.Error("failed to get host and key by DID", "err", err)
			errChan <- fmt.Errorf("failed to get host and key by DID: %w", err)
			return nil
		}

		// Set the provider status to connecting
		provider.Status = StatusAttemptingToConnect
		provider.ConnectionAttempts = backoff
		provider.Host = host
		if err := r.db.Save(provider).Error; err != nil {
			log.Error("failed to save provider state to DB", "err", err)
			errChan <- fmt.Errorf("failed to save provider state to DB: %w", err)
			return nil
		}

		// Connect to the provider's SubscribeLabels endpoint
		u := url.URL{Scheme: "wss", Host: provider.Host, Path: "/xrpc/com.atproto.label.subscribeLabels"}
		if provider.Cursor != 0 {
			q := u.Query()
			q.Set("cursor", fmt.Sprintf("%d", provider.Cursor))
			u.RawQuery = q.Encode()
		}

		log.Info("connecting to label subscription socket", "url", u.String(), "cursor", provider.Cursor, "backoff", backoff)

		d := websocket.DefaultDialer
		con, res, err := d.DialContext(ctx, u.String(), http.Header{
			"User-Agent": []string{"vortex/0.0.1"},
		})
		if err != nil {
			status := -1
			if res != nil {
				status = res.StatusCode
			}
			log.Warn("dialing failed", "err", err, "backoff", backoff, "status", status)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				log.Warn("label provider does not appear to be online, we'll keep retrying anyway though", "backoff", backoff)
				// if err := r.db.Model(&LabelProvider{}).Where("id = ?", provider.ID).Update("banned", true).Error; err != nil {
				// 	log.Error("failed to ban failing label provider", "err", err)
				// }
				// return nil
			}

			continue
		}

		log.Info("label subscription socket connected", "status", res.StatusCode)

		provider.Status = StatusConnected
		connectedAt := time.Now()
		provider.LastConnected = &connectedAt

		// Save the provider state to the DB
		if err := r.db.Save(provider).Error; err != nil {
			log.Error("failed to save provider state to DB", "err", err)
			errChan <- fmt.Errorf("failed to save provider state to DB: %w", err)
			return nil
		}

		curCursor := provider.Cursor
		if err := events.HandleRepoStream(ctx, con, scheduler); err != nil {
			log.Error("label provider connection errored", "err", err)
		}

		// Load the provider's cursor from the DB
		if err := r.db.First(provider, provider.ID).Error; err != nil {
			log.Error("failed to load provider from DB", "err", err)
			errChan <- fmt.Errorf("failed to load provider from DB: %w", err)
			return nil
		}

		// We made progress, so the labeler is alive.
		if provider.Cursor > curCursor {
			backoff = 0
		}
	}
}

// DisconnectProvider disconnects a label provider
func (r *LabelRegistry) DisconnectProvider(did string) error {
	// Load the provider from the DB
	dbProvider := r.GetProviderByDID(did)
	if dbProvider == nil {
		return fmt.Errorf("label provider not found: %s", did)
	}

	r.logger.Info("disconnecting label provider", "provider", dbProvider.DID)

	// Find the in-memory provider
	var memProvider *LabelProvider
	r.provLk.RLock()
	for _, prov := range r.Providers {
		if prov.DID == did {
			memProvider = prov
			break
		}
	}
	r.provLk.RUnlock()

	if memProvider != nil {
		close(memProvider.signalShutdown)
		<-memProvider.shutdownComplete
	}

	// Remove the provider from the in-memory list
	r.provLk.Lock()
	for i, prov := range r.Providers {
		if prov.DID == did {
			r.Providers = append(r.Providers[:i], r.Providers[i+1:]...)
			break
		}
	}
	r.provLk.Unlock()

	// Set the provider to not autoconnect
	if err := r.db.Model(&LabelProvider{}).Where("id = ?", dbProvider.ID).Update("autoconnect", false).Error; err != nil {
		return fmt.Errorf("failed to set provider to not autoconnect: %w", err)
	}

	r.logger.Info("label provider disconnected and autoconnect disabled", "provider", dbProvider.DID)
	return nil
}

// DeleteProvider deletes a label provider
func (r *LabelRegistry) DeleteProvider(did string) error {
	// Load the provider from the DB
	dbProvider := r.GetProviderByDID(did)
	if dbProvider == nil {
		return fmt.Errorf("label provider not found: %s", did)
	}

	r.logger.Info("deleting label provider", "provider", dbProvider.DID)

	// Find the in-memory provider
	var memProvider *LabelProvider
	r.provLk.RLock()
	for _, prov := range r.Providers {
		if prov.DID == did {
			memProvider = prov
			break
		}
	}
	r.provLk.RUnlock()

	if memProvider != nil {
		close(memProvider.signalShutdown)
		<-memProvider.shutdownComplete
	}

	// Remove the provider from the in-memory list
	r.provLk.Lock()
	for i, prov := range r.Providers {
		if prov.DID == did {
			r.Providers = append(r.Providers[:i], r.Providers[i+1:]...)
			break
		}
	}
	r.provLk.Unlock()

	// Delete the provider from the DB (not soft delete)
	if err := r.db.Unscoped().Delete(&LabelProvider{}, dbProvider.ID).Error; err != nil {
		return fmt.Errorf("failed to delete provider from DB: %w", err)
	}

	r.logger.Info("label provider deleted", "provider", dbProvider.DID)
	return nil
}

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 10 {
		return (time.Duration(b) * 2) + (time.Millisecond * time.Duration(rand.Intn(1000)))
	}

	return time.Second * 60
}

func windowFunc() (slidingwindow.Window, slidingwindow.StopFunc) {
	return slidingwindow.NewLocalWindow()
}

// Limiters holds the rate limiters for a label provider
type Limiters struct {
	PerSecond *slidingwindow.Limiter
	PerHour   *slidingwindow.Limiter
	PerDay    *slidingwindow.Limiter
}

// GetLimiters returns the rate limiters for a label provider
func (r *LabelRegistry) GetLimiters(providerID uint) *Limiters {
	r.LimitMux.RLock()
	defer r.LimitMux.RUnlock()
	return r.Limiters[providerID]
}

// GetOrCreateLimiters returns the rate limiters for a label provider, creating them if they don't exist
func (r *LabelRegistry) GetOrCreateLimiters(providerID uint, perSecLimit int64, perHourLimit int64, perDayLimit int64) *Limiters {
	r.LimitMux.RLock()
	defer r.LimitMux.RUnlock()
	lim, ok := r.Limiters[providerID]
	if !ok {
		perSec, _ := slidingwindow.NewLimiter(time.Second, perSecLimit, windowFunc)
		perHour, _ := slidingwindow.NewLimiter(time.Hour, perHourLimit, windowFunc)
		perDay, _ := slidingwindow.NewLimiter(time.Hour*24, perDayLimit, windowFunc)
		lim = &Limiters{
			PerSecond: perSec,
			PerHour:   perHour,
			PerDay:    perDay,
		}
		r.Limiters[providerID] = lim
	}

	return lim
}

// SetLimits sets the rate limits for a label provider
func (r *LabelRegistry) SetLimits(providerID uint, perSecLimit int64, perHourLimit int64, perDayLimit int64) {
	r.LimitMux.Lock()
	defer r.LimitMux.Unlock()
	lim, ok := r.Limiters[providerID]
	if !ok {
		perSec, _ := slidingwindow.NewLimiter(time.Second, perSecLimit, windowFunc)
		perHour, _ := slidingwindow.NewLimiter(time.Hour, perHourLimit, windowFunc)
		perDay, _ := slidingwindow.NewLimiter(time.Hour*24, perDayLimit, windowFunc)
		lim = &Limiters{
			PerSecond: perSec,
			PerHour:   perHour,
			PerDay:    perDay,
		}
		r.Limiters[providerID] = lim
	}

	lim.PerSecond.SetLimit(perSecLimit)
	lim.PerHour.SetLimit(perHourLimit)
	lim.PerDay.SetLimit(perDayLimit)
}
