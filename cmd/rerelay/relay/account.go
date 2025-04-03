package relay

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"

	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

func (r *Relay) GetAccount(ctx context.Context, did syntax.DID) (*models.Account, error) {
	ctx, span := tracer.Start(ctx, "GetAccount")
	defer span.End()

	// first try cache
	a, ok := r.accountCache.Get(did.String())
	if ok {
		return a, nil
	}

	var acc models.Account
	if err := r.db.Where("did = ?", did).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	// TODO: is this zero UID check redundant?
	if acc.UID == 0 {
		return nil, ErrAccountNotFound
	}

	r.accountCache.Add(did.String(), &acc)

	return &acc, nil
}

func (r *Relay) GetAccountRepo(ctx context.Context, uid uint64) (*models.AccountRepo, error) {
	var repo models.AccountRepo
	if err := r.db.First(&repo, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountRepoNotFound
		}
		// TODO: log here?
		return nil, err
	}
	return &repo, nil
}

/* XXX: refactor in to syncHostAccount? */
func (r *Relay) CreateAccount(ctx context.Context, host *models.Host, did syntax.DID) (*models.Account, error) {
	newUsersDiscovered.Inc()
	start := time.Now()
	account, err := r.syncHostAccount(ctx, did, host, nil)
	newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, fmt.Errorf("fed event create external user: %w", err)
	}
	return account, nil
}

// syncHostAccount ensures that a DID has an account record in the database attached to a Host record in the database
// Some fields may be updated if needed.
// did is the user
// host is the Host we received this from, not necessarily the canonical Host in the DID document
// cachedAccount is (optionally) the account that we have already looked up from cache or database
func (r *Relay) syncHostAccount(ctx context.Context, did syntax.DID, host *models.Host, cachedAccount *models.Account) (*models.Account, error) {
	ctx, span := tracer.Start(ctx, "syncHostAccount")
	defer span.End()

	externalUserCreationAttempts.Inc()

	r.Logger.Debug("create external user", "did", did)

	// lookup identity so that we know a DID's canonical source Host
	ident, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("no ident for did %s, %w", did, err)
	}
	pdsEndpoint := ident.PDSEndpoint()
	if pdsEndpoint == "" {
		return nil, fmt.Errorf("account has no PDS endpoint registered: %s", did)
	}
	durl, err := url.Parse(pdsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("account has bad url (%#v): %w", pdsEndpoint, err)
	}

	// is the canonical Host banned?
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

	var canonicalHost *models.Host
	if host.Hostname == durl.Host {
		// we got the message from the canonical Host, convenient!
		canonicalHost = host
	} else {
		// we got the message from an intermediate relay
		// check our db for info on canonical Host
		// XXX: rename "peering"
		var peering models.Host
		if err := r.db.Find(&peering, "hostname = ?", durl.Host).Error; err != nil {
			r.Logger.Error("failed to find host", "host", durl.Host)
			return nil, err
		}
		canonicalHost = &peering
	}

	if canonicalHost.Status == models.HostStatusBanned {
		return nil, fmt.Errorf("refusing to create user with banned Host")
	}

	if canonicalHost.ID == 0 {
		// we got an event from a non-canonical Host (an intermediate relay)
		// a non-canonical Host we haven't seen before; ping it to make sure it's real
		// TODO: what do we actually want to track about the source we immediately got this message from vs the canonical Host?
		r.Logger.Warn("new host discovered in create user flow", "pds", durl.String(), "did", did)

		err = r.HostChecker.CheckHost(ctx, durl.String())
		if err != nil {
			// TODO: failing this shouldn't halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// could check other things, a valid response is good enough for now
		canonicalHost.Hostname = durl.Host
		canonicalHost.NoSSL = !(durl.Scheme == "https")
		// XXX canonicalHost.RateLimit = float64(r.Slurper.Config.DefaultPerSecondLimit)
		// XXX canonicalHost.HourlyEventLimit = r.Slurper.Config.DefaultPerHourLimit
		// XXX canonicalHost.DailyEventLimit = r.Slurper.Config.DefaultPerDayLimit
		canonicalHost.AccountLimit = r.Slurper.Config.DefaultRepoLimit

		if r.Config.SSL && canonicalHost.NoSSL {
			return nil, fmt.Errorf("did references non-ssl Host, this is disallowed in prod: %q %q", did, pdsEndpoint)
		}

		if err := r.db.Create(&canonicalHost).Error; err != nil {
			return nil, err
		}
	}

	if canonicalHost.ID == 0 {
		panic("somehow failed to create a pds entry?")
	}

	if canonicalHost.AccountCount >= canonicalHost.AccountLimit {
		// TODO: soft-limit / hard-limit ? create account in 'throttled' state, unless there are _really_ too many accounts
		return nil, fmt.Errorf("refusing to create user on Host at max repo limit for pds %q", canonicalHost.Hostname)
	}

	// this lock just governs the lower half of this function
	r.accountLk.Lock()
	defer r.accountLk.Unlock()

	if cachedAccount == nil {
		cachedAccount, err = r.GetAccount(ctx, did)
	}
	if errors.Is(err, ErrAccountNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if cachedAccount != nil {
		// XXX: caHost := cachedAccount.GetHost()
		caHost := cachedAccount.HostID
		if caHost != canonicalHost.ID {
			// Account is now on a different Host, update
			err = r.db.Transaction(func(tx *gorm.DB) error {
				if caHost != 0 {
					// decrement prior Host's account count
					tx.Model(&models.Host{}).Where("id = ?", caHost).Update("account_count", gorm.Expr("account_count - 1"))
				}
				// update user's Host ID
				res := tx.Model(models.Account{}).Where("uid = ?", cachedAccount.UID).Update("host_id", canonicalHost.ID)
				if res.Error != nil {
					return fmt.Errorf("failed to update users pds: %w", res.Error)
				}
				// increment new Host's account count
				res = tx.Model(&models.Host{}).Where("id = ? AND account_count < account_limit", canonicalHost.ID).Update("account_count", gorm.Expr("account_count + 1"))
				return nil
			})

			// XXX: cachedAccount.SetHost(canonicalHost.ID)
			cachedAccount.HostID = canonicalHost.ID

			// flush account cache
			r.accountCache.Remove(did.String())
		}
		return cachedAccount, nil
	}

	newAccount := models.Account{
		DID:            did.String(),
		HostID:         canonicalHost.ID,
		Status:         models.AccountStatusActive,
		UpstreamStatus: models.AccountStatusActive,
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.Host{}).Where("id = ? AND account_count < account_limit", canonicalHost.ID).Update("account_count", gorm.Expr("account_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("failed to increment repo count for pds %q: %w", canonicalHost.Hostname, res.Error)
		}
		if terr := tx.Create(&newAccount).Error; terr != nil {
			r.Logger.Error("failed to create user", "did", newAccount.DID, "err", terr)
			return fmt.Errorf("failed to create other pds user: %w", terr)
		}
		return nil
	})
	if err != nil {
		r.Logger.Error("user create and pds inc err", "err", err)
		return nil, err
	}

	r.accountCache.Add(did.String(), &newAccount)

	return &newAccount, nil
}

func (r *Relay) UpdateAccountStatus(ctx context.Context, did syntax.DID, status models.AccountStatus) error {
	acc, err := r.GetAccount(ctx, did)
	if err != nil {
		return err
	}

	if err := r.db.Model(models.Account{}).Where("uid = ?", acc.UID).Update("status", status).Error; err != nil {
		return err
	}

	// clear account cache
	r.accountCache.Remove(did.String())

	// NOTE: not wiping events for user from persister (backfill window)
	return nil
}

func (r *Relay) ListAccounts(ctx context.Context, cursor int64, limit int) ([]*models.Account, error) {

	accounts := []*models.Account{}
	if err := r.db.Model(&models.Account{}).Where("uid > ? AND status IS NOT 'takendown' AND (upstream_status IS NULL OR upstream_status = 'active')", cursor).Order("uid").Limit(limit).Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *Relay) UpsertAccountRepo(uid uint64, rev syntax.TID, commitCID, commitDataCID cid.Cid) error {
	return r.db.Exec("INSERT INTO account_repo (uid, rev, commit_cid, commit_data) VALUES (?, ?, ?, ?) ON CONFLICT (uid) DO UPDATE SET rev = EXCLUDED.rev, commit_cid = EXCLUDED.commit_cid, commit_data = EXCLUDED.commit_data", uid, rev, commitCID.String(), commitDataCID.String()).Error
}

// this function with exact name and args implements the `diskpersist.UidSource` interface
func (r *Relay) DidToUid(ctx context.Context, did string) (uint64, error) {
	// NOTE: not re-parsing DID here (this function is called "loopback" from persister)
	xu, err := r.GetAccount(ctx, syntax.DID(did))
	if err != nil {
		return 0, err
	}
	if xu == nil {
		return 0, ErrAccountNotFound
	}
	return xu.UID, nil
}
