package relay

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"

	"gorm.io/gorm"
)

func (r *Relay) GetHost(ctx context.Context, hostID uint64) (*models.Host, error) {
	ctx, span := tracer.Start(ctx, "getHost")
	defer span.End()

	var host models.Host
	if err := r.db.Find(&host, hostID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostNotFound
		}
		return nil, err
	}

	// TODO: is this further check needed?
	if host.ID == 0 {
		return nil, ErrAccountNotFound
	}

	return &host, nil
}
