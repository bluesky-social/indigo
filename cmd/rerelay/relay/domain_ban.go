package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"

	"gorm.io/gorm"
)

// XXX: tests for domain ban logic (which hit an actual database)

// DomainIsBanned checks if the given hostname is banned. It checks all domain suffixs.
//
// Hostname is assumed to have been parsed/normalized (eg, lower-case).
func (r *Relay) DomainIsBanned(ctx context.Context, hostname string) (bool, error) {

	if strings.HasPrefix(hostname, "localhost:") {
		// XXX: check localhost config separately
	}

	// otherwise we shouldn't have a port/colon
	if strings.Contains(hostname, ":") {
		return false, fmt.Errorf("unexpected colon in hostname: %s", hostname)
	}

	// try entire host, and then all domain suffixes
	segments := strings.Split(hostname, ".")
	for i := 0; i < len(segments)-1; i++ {
		dchk := strings.Join(segments[i:], ".")
		found, err := r.findDomainBan(ctx, dchk)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func (r *Relay) findDomainBan(ctx context.Context, domain string) (bool, error) {
	var ban models.DomainBan
	if err := r.db.Model(&models.DomainBan{}).Where("domain = ?", domain).First(&ban).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Relay) CreateDomainBan(ctx context.Context, domain string) error {
	domainBan := models.DomainBan{Domain: domain}
	return r.db.Create(&domainBan).Error
}

func (r *Relay) RemoveDomainBan(ctx context.Context, domain string) error {
	return r.db.Delete(&models.DomainBan{}, "domain = ?", domain).Error
}

// returns all domain bans
func (r *Relay) ListDomainBans(ctx context.Context) ([]models.DomainBan, error) {
	bans := []models.DomainBan{}
	if err := r.db.Model(&models.DomainBan{}).Find(&bans).Error; err != nil {
		return nil, err
	}
	return bans, nil
}
