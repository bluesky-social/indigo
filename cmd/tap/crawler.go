package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	lexutil "github.com/bluesky-social/indigo/lex/util"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Crawler struct {
	db     *gorm.DB
	logger *slog.Logger

	fullNetworkMode            bool
	listCollectionService      string
	signalCollection           string
	lightRailSignalCollections []string
}

func NewCrawler(logger *slog.Logger, db *gorm.DB, config *TapConfig) *Crawler {
	url := config.RelayUrl
	if config.LightRailUrl != "" {
		url = config.LightRailUrl
	}

	return &Crawler{
		logger:                     logger.With("component", "crawler"),
		db:                         db,
		fullNetworkMode:            config.FullNetworkMode,
		listCollectionService:      url,
		signalCollection:           config.SignalCollection,
		lightRailSignalCollections: config.LightRailSignalCollections,
	}
}

func (c *Crawler) Run(ctx context.Context) {
	for {
		var err error
		if c.signalCollection != "" {
			err = c.EnumerateNetworkByCollection(ctx)
		} else if len(c.lightRailSignalCollections) > 0 {
			err = c.EnumerateNetworkByCollection(ctx)
		} else if c.fullNetworkMode {
			err = c.EnumerateNetwork(ctx)
		}
		var d time.Duration
		if err != nil {
			c.logger.Error("failed to enumerate network", "error", err)
			d = time.Minute
		} else {
			c.logger.Info("finished enumerating network, sleeping for 1 day")
			d = 24 * time.Hour
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

func (c *Crawler) GetCursor(ctx context.Context) (string, error) {
	if c.signalCollection != "" {
		return c.getCollectionCursor(ctx, c.signalCollection)
	} else if len(c.lightRailSignalCollections) > 0 {
		return c.getCollectionCursor(ctx, strings.Join(c.lightRailSignalCollections, ","))
	} else if c.fullNetworkMode {
		return c.getListReposCursor(ctx)
	}
	return "", nil
}

// EnumerateNetwork discovers and tracks all repositories on the network.
func (c *Crawler) EnumerateNetwork(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "EnumerateNetwork")
	defer span.End()

	cursor, err := c.getListReposCursor(ctx)
	if err != nil {
		return err
	}

	client := atclient.NewAPIClient(c.listCollectionService)
	client.Headers.Set("User-Agent", userAgent())
	client.Client = &http.Client{
		Timeout: 30 * time.Second,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		repoList, err := comatproto.SyncListRepos(ctx, client, cursor, 1000)
		if err != nil {
			return fmt.Errorf("failed to list repos: %w", err)
		}

		repos := make([]models.Repo, 0)
		for _, repo := range repoList.Repos {
			if repo.Active != nil && *repo.Active == false {
				continue
			}
			repos = append(repos, models.Repo{
				Did:    repo.Did,
				State:  models.RepoStatePending,
				Status: models.AccountStatusActive,
			})
		}

		if len(repos) == 0 {
			break
		}

		if err := c.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&repos).Error; err != nil {
			c.logger.Error("failed to save repos batch", "error", err, "count", len(repos))
			return err
		}

		crawlerReposDiscovered.Add(float64(len(repos)))
		c.logger.Info("enumerated repos batch", "count", len(repos))

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor

		if err := c.db.WithContext(ctx).Save(&models.ListReposCursor{
			Url:    c.listCollectionService,
			Cursor: cursor,
		}).Error; err != nil {
			c.logger.Error("failed to save list repos cursor", "error", err)
		}
	}

	c.logger.Info("network enumeration complete")
	return nil
}

func (c *Crawler) getListReposCursor(ctx context.Context) (string, error) {
	var dbCursor models.ListReposCursor
	err := c.db.WithContext(ctx).Where("url = ?", c.listCollectionService).First(&dbCursor).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return "", fmt.Errorf("failed to read list repos cursor: %w", err)
		}
		return "", nil
	}
	return dbCursor.Cursor, nil
}

// EnumerateNetworkByCollection discovers repositories that have records in the specified collection.
func (c *Crawler) EnumerateNetworkByCollection(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "EnumerateNetworkByCollection")
	lightRail := len(c.lightRailSignalCollections) > 0
	var collectionCursor string
	if lightRail {
		collectionCursor = strings.Join(c.lightRailSignalCollections, ",")
	} else {
		collectionCursor = c.signalCollection
	}
	span.SetAttributes(attribute.String("collection", collectionCursor))
	defer span.End()

	cursor, err := c.getCollectionCursor(ctx, collectionCursor)
	if err != nil {
		return err
	}

	client := atclient.NewAPIClient(c.listCollectionService)
	client.Headers.Set("User-Agent", userAgent())
	client.Client = &http.Client{
		Timeout: 30 * time.Second,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var repoList *comatproto.SyncListReposByCollection_Output
		if lightRail {
			repoList, err = LightRailSyncListReposByCollection(ctx, client, c.lightRailSignalCollections, cursor, 1000)
		} else {
			repoList, err = comatproto.SyncListReposByCollection(ctx, client, c.signalCollection, cursor, 1000)
		}
		if err != nil {
			return fmt.Errorf("failed to list repos by collection: %w", err)
		}

		repos := make([]models.Repo, 0)
		for _, repo := range repoList.Repos {
			repos = append(repos, models.Repo{
				Did:    repo.Did,
				State:  models.RepoStatePending,
				Status: models.AccountStatusActive,
			})
		}

		if len(repos) == 0 {
			break
		}

		if err := c.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&repos).Error; err != nil {
			c.logger.Error("failed to save repos batch", "error", err, "collection", collectionCursor, "count", len(repos))
			return err
		}

		crawlerReposDiscovered.Add(float64(len(repos)))
		c.logger.Info("enumerated repos by collection batch", "collection", collectionCursor, "count", len(repos))

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor

		if err := c.db.WithContext(ctx).Save(&models.CollectionCursor{
			Url:        c.listCollectionService,
			Collection: collectionCursor,
			Cursor:     cursor,
		}).Error; err != nil {
			c.logger.Error("failed to save collection cursor", "error", err)
		}
	}

	c.logger.Info("collection enumeration complete", "collection", collectionCursor)
	return nil
}

func (c *Crawler) getCollectionCursor(ctx context.Context, collection string) (string, error) {
	var dbCursor models.CollectionCursor
	err := c.db.WithContext(ctx).Where("url = ? AND collection = ?", c.listCollectionService, collection).First(&dbCursor).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return "", fmt.Errorf("failed to read collection cursor: %w", err)
		}
		return "", nil
	}
	return dbCursor.Cursor, nil
}

// LightRailSyncListReposByCollection calls the XRPC method "com.atproto.sync.listReposByCollection" on a lightrail service that supports multiple collections
//
// limit: Maximum size of response set. Recommend setting a large maximum (1000+) when enumerating large DID lists.
func LightRailSyncListReposByCollection(ctx context.Context, c lexutil.LexClient, collection []string, cursor string, limit int64) (*comatproto.SyncListReposByCollection_Output, error) {
	var out comatproto.SyncListReposByCollection_Output

	params := map[string]interface{}{}
	params["collection"] = collection
	if cursor != "" {
		params["cursor"] = cursor
	}
	if limit != 0 {
		params["limit"] = limit
	}
	if err := c.LexDo(ctx, lexutil.Query, "", "com.atproto.sync.listReposByCollection", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
