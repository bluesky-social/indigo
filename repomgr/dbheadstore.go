package repomgr

import (
	"context"

	"github.com/bluesky-social/indigo/models"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func NewDbHeadStore(db *gorm.DB) *DbHeadStore {
	db.AutoMigrate(RepoHead{})

	return &DbHeadStore{db}
}

type DbHeadStore struct {
	db *gorm.DB
}

func (hs *DbHeadStore) InitUser(ctx context.Context, user models.Uid, root cid.Cid) error {
	if err := hs.db.WithContext(ctx).Create(&RepoHead{
		Usr:  user,
		Root: root.String(),
	}).Error; err != nil {
		return err
	}

	return nil
}

func (hs *DbHeadStore) UpdateUserRepoHead(ctx context.Context, user models.Uid, root cid.Cid) error {
	ctx, span := otel.Tracer("repoman").Start(ctx, "UpdateUserRepoHead")
	defer span.End()

	if err := hs.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "usr"}},
		DoUpdates: clause.AssignmentColumns([]string{"root"}),
	}).Create(&RepoHead{
		Usr:  user,
		Root: root.String(),
	}).Error; err != nil {
		return err
	}

	return nil
}

func (hs *DbHeadStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	ctx, span := otel.Tracer("repoman").Start(ctx, "GetUserRepoHead")
	defer span.End()

	var headrec RepoHead
	if err := hs.db.WithContext(ctx).Find(&headrec, "usr = ?", user).Error; err != nil {
		return cid.Undef, err
	}

	if headrec.ID == 0 {
		return cid.Undef, gorm.ErrRecordNotFound
	}

	cc, err := cid.Decode(headrec.Root)
	if err != nil {
		return cid.Undef, err
	}

	return cc, nil
}
