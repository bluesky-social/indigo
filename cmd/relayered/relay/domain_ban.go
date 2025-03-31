package relay

import (
	"context"
	"strings"

	"github.com/bluesky-social/indigo/cmd/relayered/relay/slurper"
)

// DomainIsBanned checks if the given host is banned, starting with the host
// itself, then checking every parent domain up to the tld
func (r *Relay) DomainIsBanned(ctx context.Context, host string) (bool, error) {
	// ignore ports when checking for ban status
	hostport := strings.Split(host, ":")

	segments := strings.Split(hostport[0], ".")

	// TODO: use normalize method once that merges
	var cleaned []string
	for _, s := range segments {
		if s == "" {
			continue
		}
		s = strings.ToLower(s)

		cleaned = append(cleaned, s)
	}
	segments = cleaned

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

func (r *Relay) findDomainBan(ctx context.Context, host string) (bool, error) {
	var db slurper.DomainBan
	if err := r.db.Find(&db, "domain = ?", host).Error; err != nil {
		return false, err
	}

	if db.ID == 0 {
		return false, nil
	}

	return true, nil
}
