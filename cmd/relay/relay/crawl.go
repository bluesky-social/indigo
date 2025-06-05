package relay

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
)

func (r *Relay) SubscribeToHost(ctx context.Context, hostname string, noSSL, adminForce bool) error {

	// if we already have an active subscription, exit early
	if r.Slurper.CheckIfSubscribed(hostname) {
		return nil
	}

	// fetch host info from database. this query will not error if host does not yet exist
	var host models.Host
	if err := r.db.WithContext(ctx).Find(&host, "hostname = ?", hostname).Error; err != nil {
		return err
	}

	if host.ID == 0 {

		// check if we're over the limit for new hosts today (bypass if admin mode)
		if !adminForce && !r.HostPerDayLimiter.Allow() {
			// TODO: is this the correct error code?
			return ErrNewHostsDisabled
		}

		accountLimit := r.Config.DefaultRepoLimit
		trusted := IsTrustedHostname(hostname, r.Config.TrustedDomains)
		if trusted {
			accountLimit = r.Config.TrustedRepoLimit
		}

		host = models.Host{
			Hostname:     hostname,
			NoSSL:        noSSL,
			Status:       models.HostStatusActive,
			Trusted:      trusted,
			AccountLimit: accountLimit,
		}

		if err := r.db.WithContext(ctx).Create(&host).Error; err != nil {
			return err
		}

		r.Logger.Info("adding new host subscription", "hostname", hostname, "noSSL", noSSL, "adminForce", adminForce)
	} else if host.Status == models.HostStatusBanned {
		return fmt.Errorf("cannot subscribe to banned pds")
	}

	return r.Slurper.Subscribe(&host)
}

// This function expects to be run when starting up, to re-connect to known active hosts
func (r *Relay) ResubscribeAllHosts(ctx context.Context) error {

	var all []models.Host
	if err := r.db.WithContext(ctx).Find(&all, "status = ?", "active").Error; err != nil {
		return err
	}

	for _, host := range all {
		logger := r.Logger.With("hostID", host.ID, "hostname", host.Hostname)
		logger.Info("re-subscribing to active host")
		// make a copy of host
		host := host
		err := r.Slurper.Subscribe(&host)
		if err != nil {
			logger.Warn("failed to re-subscribe to host", "err", err)
		}
		// sleep for a very short period, so we don't open tons of sockets at the same time
		time.Sleep(1 * time.Milisecond)
	}
	return nil
}
