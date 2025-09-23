package archiver

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

func NewRepoFetcher(db *gorm.DB, rm *repomgr.RepoManager, maxConcurrency int) *RepoFetcher {
	return &RepoFetcher{
		repoman:                rm,
		db:                     db,
		Limiters:               make(map[uint]*rate.Limiter),
		ApplyPDSClientSettings: func(*xrpc.Client) {},
		MaxConcurrency:         maxConcurrency,
		log:                    slog.Default().With("system", "indexer"),
	}
}

type RepoFetcher struct {
	repoman *repomgr.RepoManager
	db      *gorm.DB

	Limiters map[uint]*rate.Limiter
	LimitMux sync.RWMutex

	MaxConcurrency int

	ApplyPDSClientSettings func(*xrpc.Client)

	log *slog.Logger
}

func (rf *RepoFetcher) GetLimiter(pdsID uint) *rate.Limiter {
	rf.LimitMux.RLock()
	defer rf.LimitMux.RUnlock()

	return rf.Limiters[pdsID]
}

func (rf *RepoFetcher) GetOrCreateLimiter(pdsID uint, pdsrate float64) *rate.Limiter {
	rf.LimitMux.Lock()
	defer rf.LimitMux.Unlock()

	lim, ok := rf.Limiters[pdsID]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(pdsrate), 1)
		rf.Limiters[pdsID] = lim
	}

	return lim
}

func (rf *RepoFetcher) SetLimiter(pdsID uint, lim *rate.Limiter) {
	rf.LimitMux.Lock()
	defer rf.LimitMux.Unlock()

	rf.Limiters[pdsID] = lim
}

func (rf *RepoFetcher) fetchRepo(ctx context.Context, c *xrpc.Client, pds *models.PDS, did string, rev string) ([]byte, error) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "fetchRepo")
	defer span.End()

	span.SetAttributes(
		attribute.String("pds", pds.Host),
		attribute.String("did", did),
		attribute.String("rev", rev),
	)

	limiter := rf.GetOrCreateLimiter(pds.ID, pds.CrawlRateLimit)

	// Wait to prevent DOSing the PDS when connecting to a new stream with lots of active repos
	limiter.Wait(ctx)

	rf.log.Debug("SyncGetRepo", "did", did, "since", rev)
	// TODO: max size on these? A malicious PDS could just send us a petabyte sized repo here and kill us
	repo, err := atproto.SyncGetRepo(ctx, c, did, rev)
	if err != nil {
		reposFetched.WithLabelValues("fail").Inc()
		return nil, fmt.Errorf("failed to fetch repo (did=%s,rev=%s,host=%s): %w", did, rev, pds.Host, err)
	}
	reposFetched.WithLabelValues("success").Inc()

	return repo, nil
}

// TODO: since this function is the only place we depend on the repomanager, i wonder if this should be wired some other way?
func (rf *RepoFetcher) FetchAndIndexRepo(ctx context.Context, job *crawlWork) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "FetchAndIndexRepo")
	defer span.End()

	span.SetAttributes(attribute.Int("catchup", len(job.catchup)))

	ai := job.act

	var pds models.PDS
	if err := rf.db.First(&pds, "id = ?", ai.PDS).Error; err != nil {
		catchupEventsFailed.WithLabelValues("nopds").Inc()
		return fmt.Errorf("expected to find pds record (%d) in db for crawling one of their users: %w", ai.PDS, err)
	}

	c := models.ClientForPds(&pds)
	rf.ApplyPDSClientSettings(c)

	repo, err := rf.fetchRepo(ctx, c, &pds, ai.Did, "")
	if err != nil {
		return err
	}

	if err := rf.repoman.ImportNewRepo(ctx, ai.ID, ai.Did, bytes.NewReader(repo), nil); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to import backup repo (%s): %w", ai.Did, err)
	}

	return nil
}
