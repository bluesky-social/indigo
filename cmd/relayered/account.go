package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/models"
	"github.com/bluesky-social/indigo/cmd/relayered/slurper"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

func (svc *Service) DidToUid(ctx context.Context, did string) (models.Uid, error) {
	xu, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return 0, err
	}
	if xu == nil {
		return 0, ErrNotFound
	}
	return xu.ID, nil
}

func (svc *Service) lookupUserByDid(ctx context.Context, did string) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByDid")
	defer span.End()

	cu, ok := svc.userCache.Get(did)
	if ok {
		return cu, nil
	}

	var u slurper.Account
	if err := svc.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	svc.userCache.Add(did, &u)

	return &u, nil
}

func (svc *Service) lookupUserByUID(ctx context.Context, uid models.Uid) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByUID")
	defer span.End()

	var u slurper.Account
	if err := svc.db.Find(&u, "id = ?", uid).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func (svc *Service) newUser(ctx context.Context, host *slurper.PDS, did string) (*slurper.Account, error) {
	newUsersDiscovered.Inc()
	start := time.Now()
	account, err := svc.syncPDSAccount(ctx, did, host, nil)
	newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		repoCommitsResultCounter.WithLabelValues(host.Host, "uerr").Inc()
		return nil, fmt.Errorf("fed event create external user: %w", err)
	}
	return account, nil
}

var ErrCommitNoUser = errors.New("commit no user")

// syncPDSAccount ensures that a DID has an account record in the database attached to a PDS record in the database
// Some fields may be updated if needed.
// did is the user
// host is the PDS we received this from, not necessarily the canonical PDS in the DID document
// cachedAccount is (optionally) the account that we have already looked up from cache or database
func (svc *Service) syncPDSAccount(ctx context.Context, did string, host *slurper.PDS, cachedAccount *slurper.Account) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "syncPDSAccount")
	defer span.End()

	externalUserCreationAttempts.Inc()

	svc.log.Debug("create external user", "did", did)

	// lookup identity so that we know a DID's canonical source PDS
	pdid, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("bad did %#v, %w", did, err)
	}
	ident, err := svc.dir.LookupDID(ctx, pdid)
	if err != nil {
		return nil, fmt.Errorf("no ident for did %s, %w", did, err)
	}
	if len(ident.Services) == 0 {
		return nil, fmt.Errorf("no services for did %s", did)
	}
	pdsService, ok := ident.Services["atproto_pds"]
	if !ok {
		return nil, fmt.Errorf("no atproto_pds service for did %s", did)
	}
	durl, err := url.Parse(pdsService.URL)
	if err != nil {
		return nil, fmt.Errorf("pds bad url %#v, %w", pdsService.URL, err)
	}

	// is the canonical PDS banned?
	ban, err := svc.domainIsBanned(ctx, durl.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to check pds ban status: %w", err)
	}
	if ban {
		return nil, fmt.Errorf("cannot create user on pds with banned domain")
	}

	if strings.HasPrefix(durl.Host, "localhost:") {
		durl.Scheme = "http"
	}

	var canonicalHost *slurper.PDS
	if host.Host == durl.Host {
		// we got the message from the canonical PDS, convenient!
		canonicalHost = host
	} else {
		// we got the message from an intermediate relay
		// check our db for info on canonical PDS
		var peering slurper.PDS
		if err := svc.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
			svc.log.Error("failed to find pds", "host", durl.Host)
			return nil, err
		}
		canonicalHost = &peering
	}

	if canonicalHost.Blocked {
		return nil, fmt.Errorf("refusing to create user with blocked PDS")
	}

	if canonicalHost.ID == 0 {
		// we got an event from a non-canonical PDS (an intermediate relay)
		// a non-canonical PDS we haven't seen before; ping it to make sure it's real
		// TODO: what do we actually want to track about the source we immediately got this message from vs the canonical PDS?
		svc.log.Warn("pds discovered in new user flow", "pds", durl.String(), "did", did)

		// Do a trivial API request against the PDS to verify that it exists
		pclient := &xrpc.Client{Host: durl.String()}
		svc.config.ApplyPDSClientSettings(pclient)
		cfg, err := comatproto.ServerDescribeServer(ctx, pclient)
		if err != nil {
			// TODO: failing this shouldn't halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list doesn't matter...
		_ = cfg

		// could check other things, a valid response is good enough for now
		canonicalHost.Host = durl.Host
		canonicalHost.SSL = (durl.Scheme == "https")
		canonicalHost.RateLimit = float64(svc.slurper.DefaultPerSecondLimit)
		canonicalHost.HourlyEventLimit = svc.slurper.DefaultPerHourLimit
		canonicalHost.DailyEventLimit = svc.slurper.DefaultPerDayLimit
		canonicalHost.RepoLimit = svc.slurper.DefaultRepoLimit

		if svc.ssl && !canonicalHost.SSL {
			return nil, fmt.Errorf("did references non-ssl PDS, this is disallowed in prod: %q %q", did, pdsService.URL)
		}

		if err := svc.db.Create(&canonicalHost).Error; err != nil {
			return nil, err
		}
	}

	if canonicalHost.ID == 0 {
		panic("somehow failed to create a pds entry?")
	}

	if canonicalHost.RepoCount >= canonicalHost.RepoLimit {
		// TODO: soft-limit / hard-limit ? create account in 'throttled' state, unless there are _really_ too many accounts
		return nil, fmt.Errorf("refusing to create user on PDS at max repo limit for pds %q", canonicalHost.Host)
	}

	// this lock just governs the lower half of this function
	svc.extUserLk.Lock()
	defer svc.extUserLk.Unlock()

	if cachedAccount == nil {
		cachedAccount, err = svc.lookupUserByDid(ctx, did)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if cachedAccount != nil {
		caPDS := cachedAccount.GetPDS()
		if caPDS != canonicalHost.ID {
			// Account is now on a different PDS, update
			err = svc.db.Transaction(func(tx *gorm.DB) error {
				if caPDS != 0 {
					// decrement prior PDS's account count
					tx.Model(&slurper.PDS{}).Where("id = ?", caPDS).Update("repo_count", gorm.Expr("repo_count - 1"))
				}
				// update user's PDS ID
				res := tx.Model(slurper.Account{}).Where("id = ?", cachedAccount.ID).Update("pds", canonicalHost.ID)
				if res.Error != nil {
					return fmt.Errorf("failed to update users pds: %w", res.Error)
				}
				// increment new PDS's account count
				res = tx.Model(&slurper.PDS{}).Where("id = ? AND repo_count < repo_limit", canonicalHost.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
				return nil
			})

			cachedAccount.SetPDS(canonicalHost.ID)
		}
		return cachedAccount, nil
	}

	newAccount := slurper.Account{
		Did: did,
		PDS: canonicalHost.ID,
	}

	err = svc.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&slurper.PDS{}).Where("id = ? AND repo_count < repo_limit", canonicalHost.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("failed to increment repo count for pds %q: %w", canonicalHost.Host, res.Error)
		}
		if terr := tx.Create(&newAccount).Error; terr != nil {
			svc.log.Error("failed to create user", "did", newAccount.Did, "err", terr)
			return fmt.Errorf("failed to create other pds user: %w", terr)
		}
		return nil
	})
	if err != nil {
		svc.log.Error("user create and pds inc err", "err", err)
		return nil, err
	}

	svc.userCache.Add(did, &newAccount)

	return &newAccount, nil
}

func (svc *Service) TakeDownRepo(ctx context.Context, did string) error {
	u, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := svc.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", true).Error; err != nil {
		return err
	}
	u.SetTakenDown(true)

	if err := svc.events.TakeDownRepo(ctx, u.ID); err != nil {
		return err
	}

	return nil
}

func (svc *Service) ReverseTakedown(ctx context.Context, did string) error {
	u, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := svc.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", false).Error; err != nil {
		return err
	}
	u.SetTakenDown(false)

	return nil
}

func (svc *Service) GetRepoRoot(ctx context.Context, user models.Uid) (cid.Cid, error) {
	var prevState slurper.AccountPreviousState
	err := svc.db.First(&prevState, user).Error
	if err == nil {
		return prevState.Cid.CID, nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return cid.Cid{}, ErrUserStatusUnavailable
	} else {
		svc.log.Error("user db err", "err", err)
		return cid.Cid{}, fmt.Errorf("user prev db err, %w", err)
	}
}
