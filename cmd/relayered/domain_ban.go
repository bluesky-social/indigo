package main

import (
	"context"
	"strings"

	"github.com/bluesky-social/indigo/cmd/relayered/slurper"
)

// domainIsBanned checks if the given host is banned, starting with the host
// itself, then checking every parent domain up to the tld
func (s *Service) domainIsBanned(ctx context.Context, host string) (bool, error) {
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
		found, err := s.findDomainBan(ctx, dchk)
		if err != nil {
			return false, err
		}

		if found {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) findDomainBan(ctx context.Context, host string) (bool, error) {
	var db slurper.DomainBan
	if err := s.db.Find(&db, "domain = ?", host).Error; err != nil {
		return false, err
	}

	if db.ID == 0 {
		return false, nil
	}

	return true, nil
}
