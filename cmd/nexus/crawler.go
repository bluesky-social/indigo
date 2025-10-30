package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"github.com/bluesky-social/indigo/xrpc"
	"gorm.io/gorm"
)

type Crawler struct {
	DB        *gorm.DB
	Logger    *slog.Logger
	RelayHost string
}

// EnumerateNetwork discovers and tracks all repositories on the network.
func (c *Crawler) EnumerateNetwork(ctx context.Context) error {
	cursor, err := c.getListReposCursor(ctx)
	if err != nil {
		return err
	}

	client := &xrpc.Client{
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Host: c.RelayHost,
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

		if err := c.DB.Save(&repos).Error; err != nil {
			c.Logger.Error("failed to save repos batch", "error", err, "count", len(repos))
			return err
		}

		c.Logger.Info("enumerated repos batch", "count", len(repos))

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor

		if err := c.DB.Save(&models.ListReposCursor{
			Host:   c.RelayHost,
			Cursor: cursor,
		}).Error; err != nil {
			c.Logger.Error("failed to save list repos cursor", "error", err)
		}
	}

	c.Logger.Info("network enumeration complete")
	return nil
}

func (c *Crawler) getListReposCursor(ctx context.Context) (string, error) {
	var dbCursor models.ListReposCursor
	err := c.DB.Where("host = ?", c.RelayHost).First(&dbCursor).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return "", fmt.Errorf("failed to read list repos cursor: %w", err)
		}
		return "", nil
	}
	return dbCursor.Cursor, nil
}

// EnumerateNetworkByCollection discovers repositories that have records in the specified collection.
func (c *Crawler) EnumerateNetworkByCollection(ctx context.Context, collection string) error {
	cursor, err := c.getCollectionCursor(ctx, collection)
	if err != nil {
		return err
	}

	client := &xrpc.Client{
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		Host: c.RelayHost,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		repoList, err := comatproto.SyncListReposByCollection(ctx, client, collection, cursor, 1000)
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

		if err := c.DB.Save(&repos).Error; err != nil {
			c.Logger.Error("failed to save repos batch", "error", err, "collection", collection, "count", len(repos))
			return err
		}

		c.Logger.Info("enumerated repos by collection batch", "collection", collection, "count", len(repos))

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor

		if err := c.DB.Save(&models.CollectionCursor{
			Host:       c.RelayHost,
			Collection: collection,
			Cursor:     cursor,
		}).Error; err != nil {
			c.Logger.Error("failed to save collection cursor", "error", err)
		}
	}

	c.Logger.Info("collection enumeration complete", "collection", collection)
	return nil
}

func (c *Crawler) getCollectionCursor(ctx context.Context, collection string) (string, error) {
	var dbCursor models.CollectionCursor
	err := c.DB.Where("host = ? AND collection = ?", c.RelayHost, collection).First(&dbCursor).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return "", fmt.Errorf("failed to read collection cursor: %w", err)
		}
		return "", nil
	}
	return dbCursor.Cursor, nil
}
