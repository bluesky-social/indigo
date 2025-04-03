package relay

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
)

// Checks whether a host is allowed to be subscribed to
//
// Must be called with the slurper lock held
func (r *Relay) canSlurpHost(hostname string) bool {
	// Check if we're over the limit for new hosts today
	if !r.Slurper.NewHostPerDayLimiter.Allow() {
		return false
	}

	// Check if the host is a trusted domain
	for _, d := range r.Config.TrustedDomains {
		// If the domain starts with a *., it's a wildcard
		if strings.HasPrefix(d, "*.") {
			// Cut off the * so we have .domain.com
			if strings.HasSuffix(hostname, strings.TrimPrefix(d, "*")) {
				return true
			}
		} else {
			if hostname == d {
				return true
			}
		}
	}

	return !r.Config.DisableNewHosts
}

func (r *Relay) SubscribeToHost(hostname string, reg bool, adminOverride bool, rateOverrides *HostRates) error {

	// if we already have an active subscription going, exit early
	if r.Slurper.CheckIfSubscribed(hostname) {
		return nil
	}

	var host models.Host
	if err := r.db.Find(&host, "hostname = ?", hostname).Error; err != nil {
		return err
	}

	newHost := false

	if host.ID == 0 {
		if !adminOverride && !r.canSlurpHost(hostname) {
			return ErrNewSubsDisabled
		}
		// New PDS!
		npds := models.Host{
			Hostname:     hostname,
			NoSSL:        !r.Config.SSL,
			Status:       models.HostStatusActive,
			AccountLimit: r.Config.DefaultRepoLimit,
		}
		/* XXX
		if rateOverrides != nil {
			npds.RateLimit = float64(rateOverrides.PerSecond)
			npds.HourlyEventLimit = rateOverrides.PerHour
			npds.DailyEventLimit = rateOverrides.PerDay
			npds.RepoLimit = rateOverrides.RepoLimit
		}
		*/
		if err := r.db.Create(&npds).Error; err != nil {
			return err
		}

		newHost = true
		host = npds
	} else if host.Status == models.HostStatusBanned {
		return fmt.Errorf("cannot subscribe to banned pds")
	}

	/* XXX
	if !host.Registered && reg {
		host.Registered = true
		if err := s.db.Model(models.Host{}).Where("id = ?", host.ID).Update("registered", true).Error; err != nil {
			return err
		}
	}
	*/

	return r.Slurper.Subscribe(&host, newHost)
}

// This function expects to be run when starting up, to re-connect to known active hosts
func (r *Relay) ResubscribeAllHosts() error {

	var all []models.Host
	if err := r.db.Find(&all, "status = \"active\"").Error; err != nil {
		return err
	}

	for _, host := range all {
		// copy host
		host := host
		err := r.Slurper.Subscribe(&host, false)
		if err != nil {
			r.Logger.Warn("failed to re-subscribe to host", "hostID", host.ID, "hostname", host.Hostname, "err", err)
		}
	}
	return nil
}
