package relay

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

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

// Attempts creation of a new account associated with the given host, presumably because the account was discovered on that host's stream.
//
// If the account's identity doesn't match the host, this will fail. We only create accounts associated with hosts we already know of, not remote hosts (aka, no spidering).
func (r *Relay) CreateHostAccount(ctx context.Context, did syntax.DID, hostID uint64, hostname string) (*models.Account, error) {
	// NOTE: this method doesn't use locking. the database UNIQUE constraint should prevent duplicate account creation.
	logger := r.Logger.With("did", did, "hostname", hostname)

	//newUsersDiscovered.Inc()
	//start := time.Now()

	ident, err := r.Dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("new account identity resolution: %w", err)
	}
	pdsEndpoint := ident.PDSEndpoint()
	if pdsEndpoint == "" {
		return nil, fmt.Errorf("new account has no declared PDS: %s", did)
	}
	pdsHostname, _, err := ParseHostname(pdsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("new account PDS endpoint invalid: %s", pdsEndpoint)
	}

	if pdsHostname != hostname {
		if r.Config.SkipAccountHostCheck {
			logger.Warn("ignoring account host mismatch", "pdsHostname", pdsHostname)
		} else {
			return nil, fmt.Errorf("new account from a different host: %s", pdsHostname)
		}
	}

	// TODO: fetch the full host, apply throttling or rate-limits?

	// TODO: could be verifying upstream status here (using r.HostChecker)
	// XXX: limits/throttling; reach in to Slurper?
	acc := models.Account{
		DID:            did.String(),
		HostID:         hostID,
		Status:         models.AccountStatusActive,
		UpstreamStatus: models.AccountStatusActive,
	}

	// create Account row and increment host count in the same transaction
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Host{}).Where("id = ?", hostID).Update("account_count", gorm.Expr("account_count + 1")).Error; err != nil {
			return fmt.Errorf("failed to increment account count for host (%s): %w", hostname, err)
		}
		if err := tx.Create(&acc).Error; err != nil {
			return fmt.Errorf("failed to create account: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	r.accountCache.Add(did.String(), &acc)

	//newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
	return &acc, nil
}

// Checks if account matches provided hostID, and in the fast pass returns successfully. If not, checks if the account should be updated. If the account is now on the indicated host, it is updated, both in the database and struct via pointer.
//
// TODO: could also update to another known host, if doesn't match this hostID?
func (r *Relay) EnsureAccountHost(ctx context.Context, acc *models.Account, hostID uint64, hostname string) error {
	did := syntax.DID(acc.DID)
	logger := r.Logger.With("did", did, "hostname", hostname)

	if acc.HostID == hostID {
		return nil
	}

	ident, err := r.Dir.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("account identity resolution: %w", err)
	}
	pdsEndpoint := ident.PDSEndpoint()
	if pdsEndpoint == "" {
		return fmt.Errorf("account has no declared PDS: %s", did)
	}
	pdsHostname, _, err := ParseHostname(pdsEndpoint)
	if err != nil {
		return fmt.Errorf("account PDS endpoint invalid: %s", pdsEndpoint)
	}

	if pdsHostname != hostname {
		if r.Config.SkipAccountHostCheck {
			logger.Warn("ignoring account host mismatch", "pdsHostname", pdsHostname)
			return nil
		} else {
			return fmt.Errorf("new account from a different host: %s", pdsHostname)
		}
	}

	// TODO: could check upstream status here (using r.HostChecker)
	// TODO: for example, a moved account might go from takendown to active
	// XXX: limits/throttling; read in to Slurper?

	// create Account row and increment host count in the same transaction
	err = r.db.Transaction(func(tx *gorm.DB) error {
		// decrement old host count
		if err := tx.Model(&models.Host{}).Where("id = ?", acc.HostID).Update("account_count", gorm.Expr("account_count - 1")).Error; err != nil {
			return fmt.Errorf("failed to decrement account count for former host (%d): %w", acc.HostID, err)
		}
		// increment new host count
		if err := tx.Model(&models.Host{}).Where("id = ?", hostID).Update("account_count", gorm.Expr("account_count + 1")).Error; err != nil {
			return fmt.Errorf("failed to increment account count for host (%s): %w", hostname, err)
		}
		if err := tx.Model(models.Account{}).Where("uid = ?", acc.UID).Update("host_id", hostID).Error; err != nil {
			return fmt.Errorf("failed update account HostID: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// evict stale record from account cache
	r.accountCache.Remove(did.String())

	acc.HostID = hostID
	return nil
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

	// XXX: what status filter should be in place here? not deleted in addition to not takendown?
	accounts := []*models.Account{}
	if err := r.db.Model(&models.Account{}).Where("uid > ? AND status IS NOT 'takendown' AND (upstream_status IS NULL OR upstream_status = 'active')", cursor).Order("uid").Limit(limit).Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *Relay) ListAccountTakedowns(ctx context.Context, cursor int64, limit int) ([]*models.Account, error) {

	accounts := []*models.Account{}
	if err := r.db.Model(&models.Account{}).Where("uid > ? AND status = ?", cursor, models.AccountStatusTakendown).Order("uid").Limit(limit).Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *Relay) UpsertAccountRepo(uid uint64, rev syntax.TID, commitCID, commitDataCID string) error {
	return r.db.Exec("INSERT INTO account_repo (uid, rev, commit_cid, commit_data) VALUES (?, ?, ?, ?) ON CONFLICT (uid) DO UPDATE SET rev = EXCLUDED.rev, commit_cid = EXCLUDED.commit_cid, commit_data = EXCLUDED.commit_data", uid, rev, commitCID, commitDataCID).Error
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
