package main

import (
	"context"
	"fmt"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/bluesky-social/indigo/xrpc"
	"gorm.io/gorm"
)

func (n *Nexus) EnumerateNetwork(ctx context.Context) error {
	cursor, err := n.getListReposCursor(ctx)
	if err != nil {
		return err
	}

	client := &xrpc.Client{
		Client: &http.Client{},
		Host:   n.RelayHost,
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

		if err := n.db.Save(&repos).Error; err != nil {
			n.logger.Error("failed to save repos batch", "error", err)
			return err
		}

		n.logger.Info("enumerated repos batch", "count", len(repos))

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor

		if err := n.db.Save(&models.ListReposCursor{
			Host:   n.RelayHost,
			Cursor: cursor,
		}).Error; err != nil {
			n.logger.Error("failed to save lsit repos cursor", "error", err)
		}
	}

	n.logger.Info("network enumeration complete")
	return nil
}

func (n *Nexus) getListReposCursor(ctx context.Context) (string, error) {
	var dbCursor models.ListReposCursor
	err := n.db.Where("host = ?", n.RelayHost).First(&dbCursor).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return "", fmt.Errorf("failed to read list repos cursor: %w", err)
		}
		return "", nil
	}
	return dbCursor.Cursor, nil
}
