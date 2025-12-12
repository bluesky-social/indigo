package main

import (
	"context"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"gorm.io/gorm"
)

type RepoManager struct {
	logger *slog.Logger
	db     *gorm.DB
	IdDir  identity.Directory
	events *EventManager
}

func (rm *RepoManager) GetRepoState(ctx context.Context, did string) (*models.Repo, error) {
	var r models.Repo
	if err := rm.db.WithContext(ctx).First(&r, "did = ?", did).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		return nil, nil
	}
	return &r, nil
}

func (rm *RepoManager) UpdateRepoState(ctx context.Context, did string, state models.RepoState) error {
	return rm.db.WithContext(ctx).Model(&models.Repo{}).
		Where("did = ?", did).
		Update("state", state).Error
}

// RefreshIdentity fetches the latest identity information for a DID.
func (rm *RepoManager) RefreshIdentity(ctx context.Context, did string) error {
	ctx, span := tracer.Start(ctx, "RefreshIdentity")
	defer span.End()

	if err := rm.IdDir.Purge(ctx, syntax.DID(did).AtIdentifier()); err != nil {
		rm.logger.Error("failed to purge identity cache", "did", did, "error", err)
	}

	id, err := rm.IdDir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return err
	}

	curr, err := rm.GetRepoState(ctx, did)
	if err != nil {
		return err
	} else if curr == nil {
		return nil
	}

	handleStr := id.Handle.String()
	if handleStr == curr.Handle {
		return nil
	}

	identityEvt := &IdentityEvt{
		Did:      did,
		Handle:   handleStr,
		IsActive: curr.Status == models.AccountStatusActive,
		Status:   curr.Status,
	}
	if err := rm.events.AddIdentityEvent(ctx, identityEvt, func(tx *gorm.DB) error {
		return tx.Model(&models.Repo{}).
			Where("did = ?", did).
			Update("handle", handleStr).Error
	}); err != nil {
		rm.logger.Error("failed to update handle", "did", did, "handle", handleStr, "error", err)
		return err
	}

	return nil
}

func (rm *RepoManager) EnsureRepo(ctx context.Context, did string) error {
	if err := rm.db.WithContext(ctx).Save(&models.Repo{
		Did:    did,
		State:  models.RepoStatePending,
		Status: models.AccountStatusActive,
	}).Error; err != nil {
		rm.logger.Error("failed to auto-track repo", "did", did, "error", err)
		return err
	}
	return nil
}
