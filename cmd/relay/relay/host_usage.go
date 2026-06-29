package relay

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
)

type HostAccountLimitUsage struct {
	ID           uint64
	Hostname     string
	AccountCount int64
	AccountLimit int64
	Usage        float64
	AlertSentAt  time.Time
}

type HostAccountLimitAlertClaims struct {
	Warnings   []HostAccountLimitUsage
	Recoveries []HostAccountLimitUsage
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
		Where("account_limit > 0 AND account_count * 1.0 / account_limit >= ? AND status <> ?", threshold, models.HostStatusBanned).
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

func (r *Relay) ClaimDueAccountLimitAlerts(ctx context.Context, threshold float64, repeatInterval time.Duration, limit int) (*HostAccountLimitAlertClaims, error) {
	if threshold <= 0 || threshold > 1 {
		return nil, fmt.Errorf("account limit alert threshold must be greater than 0 and less than or equal to 1")
	}
	if repeatInterval <= 0 {
		return nil, fmt.Errorf("account limit alert repeat interval must be positive")
	}
	if limit < 0 {
		return nil, fmt.Errorf("account limit alert query limit must not be negative")
	}

	now := time.Now().UTC()
	cutoff := now.Add(-repeatInterval)

	warnings, err := r.claimDueAccountLimitWarnings(ctx, threshold, cutoff, now, limit)
	if err != nil {
		return nil, err
	}
	recoveries, err := r.claimDueAccountLimitRecoveries(ctx, threshold, now, limit)
	if err != nil {
		return nil, err
	}
	return &HostAccountLimitAlertClaims{
		Warnings:   warnings,
		Recoveries: recoveries,
	}, nil
}

func (r *Relay) claimDueAccountLimitWarnings(ctx context.Context, threshold float64, cutoff time.Time, now time.Time, limit int) ([]HostAccountLimitUsage, error) {
	var hosts []models.Host
	query := r.db.WithContext(ctx).
		Model(&models.Host{}).
		Where("account_limit > 0 AND account_count * 1.0 / account_limit >= ? AND status <> ? AND account_limit_alerts_silenced = ?", threshold, models.HostStatusBanned, false).
		Where("(account_limit_alert_state <> ? OR account_limit_alert_state IS NULL OR account_limit_alert_sent_at IS NULL OR account_limit_alert_sent_at <= ?)", models.HostAccountLimitAlertStateWarning, cutoff).
		Order("account_count * 1.0 / account_limit DESC, id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&hosts).Error; err != nil {
		return nil, err
	}

	out := make([]HostAccountLimitUsage, 0, len(hosts))
	for _, host := range hosts {
		usage := hostAccountLimitUsage(host, now)
		if usage.Usage < threshold {
			continue
		}

		result := r.db.WithContext(ctx).Model(&models.Host{}).
			Where("id = ? AND account_limit > 0 AND account_count * 1.0 / account_limit >= ? AND status <> ? AND account_limit_alerts_silenced = ?", host.ID, threshold, models.HostStatusBanned, false).
			Where("(account_limit_alert_state <> ? OR account_limit_alert_state IS NULL OR account_limit_alert_sent_at IS NULL OR account_limit_alert_sent_at <= ?)", models.HostAccountLimitAlertStateWarning, cutoff).
			Updates(map[string]any{
				"account_limit_alert_state":   models.HostAccountLimitAlertStateWarning,
				"account_limit_alert_sent_at": now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		out = append(out, usage)
	}
	return out, nil
}

func (r *Relay) claimDueAccountLimitRecoveries(ctx context.Context, threshold float64, now time.Time, limit int) ([]HostAccountLimitUsage, error) {
	var hosts []models.Host
	query := r.db.WithContext(ctx).
		Model(&models.Host{}).
		Where("account_limit > 0 AND account_count * 1.0 / account_limit < ? AND status <> ? AND account_limit_alerts_silenced = ? AND account_limit_alert_state = ?", threshold, models.HostStatusBanned, false, models.HostAccountLimitAlertStateWarning).
		Order("account_count * 1.0 / account_limit DESC, id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&hosts).Error; err != nil {
		return nil, err
	}

	out := make([]HostAccountLimitUsage, 0, len(hosts))
	for _, host := range hosts {
		usage := hostAccountLimitUsage(host, now)
		if usage.Usage >= threshold {
			continue
		}

		result := r.db.WithContext(ctx).Model(&models.Host{}).
			Where("id = ? AND account_limit > 0 AND account_count * 1.0 / account_limit < ? AND status <> ? AND account_limit_alerts_silenced = ? AND account_limit_alert_state = ?", host.ID, threshold, models.HostStatusBanned, false, models.HostAccountLimitAlertStateWarning).
			Updates(map[string]any{
				"account_limit_alert_state":   models.HostAccountLimitAlertStateOK,
				"account_limit_alert_sent_at": now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		out = append(out, usage)
	}
	return out, nil
}

func (r *Relay) RecordHostAccountLimitAlertSent(ctx context.Context, hostname string, state models.HostAccountLimitAlertState, sentAt time.Time) error {
	switch state {
	case models.HostAccountLimitAlertStateOK, models.HostAccountLimitAlertStateWarning:
	default:
		return fmt.Errorf("invalid account limit alert state: %s", state)
	}
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&models.Host{}).
		Where("hostname = ? AND account_limit_alerts_silenced = ?", hostname, false).
		Where("account_limit_alert_sent_at IS NULL OR account_limit_alert_sent_at <= ?", sentAt).
		Updates(map[string]any{
			"account_limit_alert_state":   state,
			"account_limit_alert_sent_at": sentAt,
		})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *Relay) UpdateHostAccountLimitAlertsSilenced(ctx context.Context, hostID uint64, silenced bool) error {
	updates := map[string]any{
		"account_limit_alerts_silenced": silenced,
	}
	if silenced {
		updates["account_limit_alert_state"] = models.HostAccountLimitAlertStateOK
		updates["account_limit_alert_sent_at"] = nil
	}
	result := r.db.WithContext(ctx).Model(models.Host{}).Where("id = ?", hostID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrHostNotFound
	}
	return nil
}

func hostAccountLimitUsage(h models.Host, alertSentAt time.Time) HostAccountLimitUsage {
	return HostAccountLimitUsage{
		ID:           h.ID,
		Hostname:     h.Hostname,
		AccountCount: h.AccountCount,
		AccountLimit: h.AccountLimit,
		Usage:        float64(h.AccountCount) / float64(h.AccountLimit),
		AlertSentAt:  alertSentAt,
	}
}
