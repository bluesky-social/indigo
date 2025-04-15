package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream"

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
func (r *Relay) CreateAccountHost(ctx context.Context, did syntax.DID, hostID uint64, hostname string) (*models.Account, error) {
	// NOTE: this method doesn't use locking. the database UNIQUE constraint should prevent duplicate account creation.
	logger := r.Logger.With("did", did, "hostname", hostname)

	newUsersDiscovered.Inc()
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

	// TODO: could be verifying upstream status here (using r.HostChecker); not particularly urgent because triggering event is already coming from the relevant host

	acc := models.Account{
		DID:            did.String(),
		HostID:         hostID,
		Status:         models.AccountStatusActive,
		UpstreamStatus: models.AccountStatusActive,
	}

	host, err := r.GetHostByID(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if host.AccountCount >= host.AccountLimit {
		acc.Status = models.AccountStatusHostThrottled
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

	// TODO: check new upstream status here (using r.HostChecker). In particular, a moved account might go from takendown to active

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

// This updates the account's "upstream" status (eg, at the account's PDS). Usually this is called in response to an `#account` event.
//
// The DID and UID are both required, and *must* match; it is assumed that calling code has already done an account lookup.
func (r *Relay) UpdateAccountUpstreamStatus(ctx context.Context, did syntax.DID, uid uint64, status models.AccountStatus) error {

	if err := r.db.Model(models.Account{}).Where("uid = ?", uid).Update("upstream_status", status).Error; err != nil {
		return err
	}

	// clear account cache
	r.accountCache.Remove(did.String())

	return nil
}

// This method updates the "local" account status (as opposed to "upstream" status, eg at the account's PDS).
//
// If the `emitEvent` flag is set true, a `#account` event is broadcast. This should be used for account-level takedowns.
func (r *Relay) UpdateAccountLocalStatus(ctx context.Context, did syntax.DID, status models.AccountStatus, emitEvent bool) error {
	acc, err := r.GetAccount(ctx, did)
	if err != nil {
		return err
	}

	if err := r.db.Model(models.Account{}).Where("uid = ?", acc.UID).Update("status", status).Error; err != nil {
		return err
	}

	// clear account cache
	r.accountCache.Remove(did.String())

	// update copy of row for computing public status field
	acc.Status = status

	if emitEvent {
		err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoAccount: &comatproto.SyncSubscribeRepos_Account{
				Active: acc.IsActive(),
				Did:    acc.DID,
				Status: acc.StatusField(),
				Time:   syntax.DatetimeNow().String(),
			},
			PrivUid: acc.UID,
		})
		if err != nil {
			r.Logger.Error("failed to emit #account event after status change", "did", did, "newStatus", status, "error", err)
			return fmt.Errorf("failed to broadcast #account event: %w", err)
		}
	}

	return nil
}

// Returns the of active accounts (based on local and upstream status). The sort order is by UID, ascending.
func (r *Relay) ListAccounts(ctx context.Context, cursor int64, limit int) ([]*models.Account, error) {

	accounts := []*models.Account{}
	if err := r.db.Model(&models.Account{}).Where("uid > ? AND status = 'active' AND upstream_status = 'active'", cursor).Order("uid").Limit(limit).Find(&accounts).Error; err != nil {
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

// This implements the `diskpersist.UidSource` interface
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

// In the general case, DIDs are case-sensitive. But PLC and did:web should not be, and should normalize to lower-case.
func NormalizeDID(orig syntax.DID) syntax.DID {
	lower := strings.ToLower(string(orig))
	if strings.HasPrefix(lower, "did:plc:") || strings.HasPrefix(lower, "did:web:") {
		return syntax.DID(lower)
	}
	return orig
}
