package relay

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

	"gorm.io/gorm"
)

func (r *Relay) GetHost(ctx context.Context, hostname string) (*models.Host, error) {
	ctx, span := tracer.Start(ctx, "getHost")
	defer span.End()

	var host models.Host
	if err := r.db.Model(models.Host{}).Where("hostname = ?", hostname).First(&host).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostNotFound
		}
		return nil, err
	}

	// TODO: is this further check needed?
	if host.ID == 0 {
		return nil, ErrHostNotFound
	}

	return &host, nil
}

func (r *Relay) GetHostByID(ctx context.Context, hostID uint64) (*models.Host, error) {
	ctx, span := tracer.Start(ctx, "getHostByID")
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
		return nil, ErrHostNotFound
	}

	return &host, nil
}

func (r *Relay) ListHosts(ctx context.Context, cursor int64, limit int) ([]*models.Host, error) {

	// TODO: filter based on active status?
	hosts := []*models.Host{}
	if err := r.db.Model(&models.Host{}).Where("id > ?", cursor).Order("id").Limit(limit).Find(&hosts).Error; err != nil {
		return nil, err
	}
	return hosts, nil
}

func (r *Relay) UpdateHostStatus(ctx context.Context, hostID uint64, status models.HostStatus) error {
	return r.db.Model(models.Host{}).Where("id = ?", hostID).Update("status", status).Error
}

// Persists all the host cursors in a single database transaction
//
// Note that in some situations this may have partial success.
func (r *Relay) PersistHostCursors(ctx context.Context, cursors *[]HostCursor) error {
	tx := r.db.WithContext(ctx).Begin()
	for _, cur := range *cursors {
		if err := tx.WithContext(ctx).Model(models.Host{}).Where("id = ?", cur.HostID).UpdateColumn("last_seq", cur.LastSeq).Error; err != nil {
			r.Logger.Error("failed to persist host cursor", "hostID", cur.HostID, "lastSeq", cur.LastSeq)
		}
	}
	return tx.WithContext(ctx).Commit().Error
}

// parses, normalizes, and validates a raw URL (HTTP or WebSocket) in to a hostname for subscriptions
//
// Hostnames must be DNS names, not IP addresses.
func ParseHostname(raw string) (hostname string, noSSL bool, err error) {

	// handle case of bare hostname
	if !strings.Contains(raw, "://") {
		if strings.HasPrefix(raw, "localhost:") {
			raw = "http://" + raw
		} else {
			raw = "https://" + raw
		}
	}

	u, err := url.Parse(raw)
	noSSL = false
	switch u.Scheme {
	case "https", "wss":
		// pass
	case "http", "ws":
		noSSL = true
	default:
		return "", false, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	// 'localhost' (exact string) is allowed *with* a required port number; SSL is optional
	if u.Hostname() == "localhost" {
		if u.Port() == "" || !strings.HasPrefix(u.Host, "localhost:") {
			return "", false, fmt.Errorf("port number is required for localhost")
		}
		return u.Host, noSSL, nil
	}

	// port numbers not allowed otherwise
	if u.Port() != "" {
		return "", false, fmt.Errorf("port number not allowed for non-local names")
	}

	// check it is a real hostname (eg, not IP address or single-word alias)
	// TODO: more SSRF protection here? eg disallow '.local'
	h, err := syntax.ParseHandle(u.Host)
	if err != nil {
		return "", false, fmt.Errorf("not a public hostname")
	}
	// lower-case in reponse
	return h.Normalize().String(), noSSL, nil
}

func IsTrustedHostname(hostname string, domains []string) bool {
	for _, d := range domains {
		if hostname == d {
			return true
		}
		if strings.HasPrefix(d, "*") && strings.HasSuffix(hostname, d[1:]) {
			return true
		}
	}
	return false
}
