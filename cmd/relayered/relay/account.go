package relay

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/slurper"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

var (
	ErrAccountNotFound        = errors.New("account not found")
	ErrAccountLastUnavailable = errors.New("account last commit not available")
	ErrCommitNoUser           = errors.New("commit no user") // TODO
)

func (r *Relay) DidToUid(ctx context.Context, did string) (uint64, error) {
	xu, err := r.LookupUserByDid(ctx, did)
	if err != nil {
		return 0, err
	}
	if xu == nil {
		return 0, ErrAccountNotFound
	}
	return xu.ID, nil
}

func (r *Relay) LookupUserByDid(ctx context.Context, did string) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByDid")
	defer span.End()

	cu, ok := r.userCache.Get(did)
	if ok {
		return cu, nil
	}

	var u slurper.Account
	if err := r.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	r.userCache.Add(did, &u)

	return &u, nil
}

func (r *Relay) LookupUserByUID(ctx context.Context, uid uint64) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByUID")
	defer span.End()

	var u slurper.Account
	if err := r.db.Find(&u, "id = ?", uid).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func (r *Relay) newUser(ctx context.Context, host *slurper.PDS, did string) (*slurper.Account, error) {
	newUsersDiscovered.Inc()
	start := time.Now()
	account, err := r.syncPDSAccount(ctx, did, host, nil)
	newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		repoCommitsResultCounter.WithLabelValues(host.Host, "uerr").Inc()
		return nil, fmt.Errorf("fed event create external user: %w", err)
	}
	return account, nil
}

// syncPDSAccount ensures that a DID has an account record in the database attached to a PDS record in the database
// Some fields may be updated if needed.
// did is the user
// host is the PDS we received this from, not necessarily the canonical PDS in the DID document
// cachedAccount is (optionally) the account that we have already looked up from cache or database
func (r *Relay) syncPDSAccount(ctx context.Context, did string, host *slurper.PDS, cachedAccount *slurper.Account) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "syncPDSAccount")
	defer span.End()

	externalUserCreationAttempts.Inc()

	r.Logger.Debug("create external user", "did", did)

	// lookup identity so that we know a DID's canonical source PDS
	pdid, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("bad did %#v, %w", did, err)
	}
	ident, err := r.dir.LookupDID(ctx, pdid)
	if err != nil {
		return nil, fmt.Errorf("no ident for did %s, %w", did, err)
	}
	if len(ident.Services) == 0 {
		return nil, fmt.Errorf("no services for did %s", did)
	}
	pdsRelay, ok := ident.Services["atproto_pds"]
	if !ok {
		return nil, fmt.Errorf("no atproto_pds service for did %s", did)
	}
	durl, err := url.Parse(pdsRelay.URL)
	if err != nil {
		return nil, fmt.Errorf("pds bad url %#v, %w", pdsRelay.URL, err)
	}

	// is the canonical PDS banned?
	ban, err := r.DomainIsBanned(ctx, durl.Host)
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
		if err := r.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
			r.Logger.Error("failed to find pds", "host", durl.Host)
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
		r.Logger.Warn("pds discovered in new user flow", "pds", durl.String(), "did", did)

		// Do a trivial API request against the PDS to verify that it exists
		pclient := &xrpc.Client{Host: durl.String()}
		if r.Config.ApplyPDSClientSettings != nil {
			r.Config.ApplyPDSClientSettings(pclient)
		}
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
		canonicalHost.RateLimit = float64(r.Slurper.DefaultPerSecondLimit)
		canonicalHost.HourlyEventLimit = r.Slurper.DefaultPerHourLimit
		canonicalHost.DailyEventLimit = r.Slurper.DefaultPerDayLimit
		canonicalHost.RepoLimit = r.Slurper.DefaultRepoLimit

		if r.Config.SSL && !canonicalHost.SSL {
			return nil, fmt.Errorf("did references non-ssl PDS, this is disallowed in prod: %q %q", did, pdsRelay.URL)
		}

		if err := r.db.Create(&canonicalHost).Error; err != nil {
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
	r.extUserLk.Lock()
	defer r.extUserLk.Unlock()

	if cachedAccount == nil {
		cachedAccount, err = r.LookupUserByDid(ctx, did)
	}
	if errors.Is(err, ErrAccountNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if cachedAccount != nil {
		caPDS := cachedAccount.GetPDS()
		if caPDS != canonicalHost.ID {
			// Account is now on a different PDS, update
			err = r.db.Transaction(func(tx *gorm.DB) error {
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

	err = r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&slurper.PDS{}).Where("id = ? AND repo_count < repo_limit", canonicalHost.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("failed to increment repo count for pds %q: %w", canonicalHost.Host, res.Error)
		}
		if terr := tx.Create(&newAccount).Error; terr != nil {
			r.Logger.Error("failed to create user", "did", newAccount.Did, "err", terr)
			return fmt.Errorf("failed to create other pds user: %w", terr)
		}
		return nil
	})
	if err != nil {
		r.Logger.Error("user create and pds inc err", "err", err)
		return nil, err
	}

	r.userCache.Add(did, &newAccount)

	return &newAccount, nil
}

func (r *Relay) TakeDownRepo(ctx context.Context, did string) error {
	u, err := r.LookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := r.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", true).Error; err != nil {
		return err
	}
	u.SetTakenDown(true)

	// NOTE: not wiping events for user from backfill window

	return nil
}

func (r *Relay) ReverseTakedown(ctx context.Context, did string) error {
	u, err := r.LookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := r.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", false).Error; err != nil {
		return err
	}
	u.SetTakenDown(false)

	return nil
}

func (r *Relay) GetAccountPreviousState(ctx context.Context, uid uint64) (*slurper.AccountPreviousState, error) {
	var prevState slurper.AccountPreviousState
	if err := r.db.First(&prevState, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountLastUnavailable
		}
		r.Logger.Error("user db err", "err", err)
		return nil, err
	}
	return &prevState, nil
}

func (r *Relay) GetRepoRoot(ctx context.Context, uid uint64) (cid.Cid, error) {
	var prevState slurper.AccountPreviousState
	err := r.db.First(&prevState, uid).Error
	if err == nil {
		return prevState.Cid.CID, nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return cid.Cid{}, ErrAccountLastUnavailable
	} else {
		r.Logger.Error("user db err", "err", err)
		return cid.Cid{}, fmt.Errorf("user prev db err, %w", err)
	}
}

func (r *Relay) GetHostForDID(ctx context.Context, did string) (string, error) {
	var pdsHostname string
	// TODO: use gorm, not "Raw"
	err := r.db.Raw("SELECT pds.host FROM users JOIN pds ON users.pds = pds.id WHERE users.did = ?", did).Scan(&pdsHostname).Error
	if err != nil {
		return "", err
	}
	return pdsHostname, nil
}

func (r *Relay) ListAccounts(ctx context.Context, cursor int64, limit int) ([]*slurper.Account, error) {

	accounts := []*slurper.Account{}
	if err := r.db.Model(&slurper.Account{}).Where("id > ? AND NOT taken_down AND (upstream_status IS NULL OR upstream_status = 'active')", cursor).Order("id").Limit(limit).Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}
