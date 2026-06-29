package relay

import (
	"context"
	"fmt"
	"sort"

	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
)

type HostAccountLimitUsage struct {
	ID           uint64
	Hostname     string
	AccountCount int64
	AccountLimit int64
	Usage        float64
}

func (r *Relay) ListHostsApproachingAccountLimit(ctx context.Context, threshold float64, limit int) ([]HostAccountLimitUsage, error) {
	if threshold <= 0 || threshold > 1 {
		return nil, fmt.Errorf("account limit alert threshold must be greater than 0 and less than or equal to 1")
	}
	if limit < 0 {
		return nil, fmt.Errorf("account limit alert query limit must not be negative")
	}

	var hosts []models.Host
	query := r.db.WithContext(ctx).
		Model(&models.Host{}).
		Where("account_limit > 0 AND account_count >= account_limit * ? AND status <> ?", threshold, models.HostStatusBanned).
		Order("account_count * 1.0 / account_limit DESC, id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&hosts).Error; err != nil {
		return nil, err
	}

	out := make([]HostAccountLimitUsage, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, HostAccountLimitUsage{
			ID:           h.ID,
			Hostname:     h.Hostname,
			AccountCount: h.AccountCount,
			AccountLimit: h.AccountLimit,
			Usage:        float64(h.AccountCount) / float64(h.AccountLimit),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Usage == out[j].Usage {
			return out[i].ID < out[j].ID
		}
		return out[i].Usage > out[j].Usage
	})

	return out, nil
}
