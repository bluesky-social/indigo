package archiver

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/models"

	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

func (s *Archiver) GetUserOrMissing(ctx context.Context, did string) (*User, error) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "getUserOrMissing")
	defer span.End()

	ai, err := s.LookupUserByDid(ctx, did)
	if err == nil {
		return ai, nil
	}

	if !isNotFound(err) {
		return nil, err
	}

	// unknown user... create it and send it off to the crawler
	return s.createMissingUserRecord(ctx, did)
}

func (s *Archiver) createMissingUserRecord(ctx context.Context, did string) (*User, error) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "createMissingUserRecord")
	defer span.End()

	externalUserCreationAttempts.Inc()

	ai, err := s.handleUserUpdate(ctx, did)
	if err != nil {
		return nil, err
	}

	if err := s.addUserToCrawler(ctx, ai); err != nil {
		return nil, fmt.Errorf("failed to add unknown user to crawler: %w", err)
	}

	return ai, nil
}

func (s *Archiver) addUserToCrawler(ctx context.Context, ai *User) error {
	s.log.Debug("Sending user to crawler: ", "did", ai.Did)
	if s.crawler == nil {
		return nil
	}

	return s.crawler.Crawl(ctx, ai)
}

func (s *Archiver) DidForUser(ctx context.Context, uid models.Uid) (string, error) {
	var ai User
	if err := s.db.First(&ai, "id = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (s *Archiver) LookupUser(ctx context.Context, id models.Uid) (*User, error) {
	var ai User
	if err := s.db.First(&ai, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (s *Archiver) LookupUserByDid(ctx context.Context, did string) (*User, error) {
	var ai User
	if err := s.db.Find(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if ai.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &ai, nil
}

func isNotFound(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}

	return false
}
