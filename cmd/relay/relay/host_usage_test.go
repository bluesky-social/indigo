package relay

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testRelayWithHostDB(t *testing.T) (*Relay, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Host{}))
	return &Relay{db: db}, db
}

func TestListHostsApproachingAccountLimit(t *testing.T) {
	r, db := testRelayWithHostDB(t)

	hosts := []models.Host{
		{Hostname: "below.example.com", AccountCount: 79, AccountLimit: 100, Status: models.HostStatusActive},
		{Hostname: "at.example.com", AccountCount: 80, AccountLimit: 100, Status: models.HostStatusActive},
		{Hostname: "over.example.com", AccountCount: 45, AccountLimit: 50, Status: models.HostStatusActive},
		{Hostname: "banned.example.com", AccountCount: 100, AccountLimit: 100, Status: models.HostStatusBanned},
	}
	for _, host := range hosts {
		require.NoError(t, db.Create(&host).Error)
	}

	usages, err := r.ListHostsApproachingAccountLimit(context.Background(), 0.80, 10)
	require.NoError(t, err)
	require.Len(t, usages, 2)

	assert.Equal(t, "over.example.com", usages[0].Hostname)
	assert.Equal(t, int64(45), usages[0].AccountCount)
	assert.Equal(t, int64(50), usages[0].AccountLimit)
	assert.Equal(t, 0.90, usages[0].Usage)
	assert.Equal(t, "at.example.com", usages[1].Hostname)
}

func TestClaimDueAccountLimitAlertsPersistsWarningState(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	require.NoError(t, db.Create(&models.Host{
		Hostname:     "over.example.com",
		AccountCount: 85,
		AccountLimit: 100,
		Status:       models.HostStatusActive,
	}).Error)

	claims, err := r.ClaimDueAccountLimitAlerts(ctx, 0.8, time.Hour, 0)
	require.NoError(t, err)
	require.Len(t, claims.Warnings, 1)
	require.Empty(t, claims.Recoveries)
	assert.Equal(t, "over.example.com", claims.Warnings[0].Hostname)
	assert.False(t, claims.Warnings[0].AlertSentAt.IsZero())

	claims, err = r.ClaimDueAccountLimitAlerts(ctx, 0.8, time.Hour, 0)
	require.NoError(t, err)
	require.Empty(t, claims.Warnings)
	require.Empty(t, claims.Recoveries)

	var host models.Host
	require.NoError(t, db.First(&host, "hostname = ?", "over.example.com").Error)
	assert.Equal(t, models.HostAccountLimitAlertStateWarning, host.AccountLimitAlertState)
	require.NotNil(t, host.AccountLimitAlertSentAt)
}

func TestClaimDueAccountLimitAlertsRepeatsAfterInterval(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	oldSentAt := time.Now().Add(-2 * time.Hour)
	require.NoError(t, db.Create(&models.Host{
		Hostname:                "over.example.com",
		AccountCount:            85,
		AccountLimit:            100,
		Status:                  models.HostStatusActive,
		AccountLimitAlertState:  models.HostAccountLimitAlertStateWarning,
		AccountLimitAlertSentAt: &oldSentAt,
	}).Error)

	claims, err := r.ClaimDueAccountLimitAlerts(ctx, 0.8, time.Hour, 0)
	require.NoError(t, err)
	require.Len(t, claims.Warnings, 1)
	require.Empty(t, claims.Recoveries)
}

func TestClaimDueAccountLimitAlertsClaimsRecovery(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	sentAt := time.Now().Add(-time.Hour)
	require.NoError(t, db.Create(&models.Host{
		Hostname:                "recovered.example.com",
		AccountCount:            20,
		AccountLimit:            100,
		Status:                  models.HostStatusActive,
		AccountLimitAlertState:  models.HostAccountLimitAlertStateWarning,
		AccountLimitAlertSentAt: &sentAt,
	}).Error)

	claims, err := r.ClaimDueAccountLimitAlerts(ctx, 0.8, time.Hour, 0)
	require.NoError(t, err)
	require.Empty(t, claims.Warnings)
	require.Len(t, claims.Recoveries, 1)
	assert.Equal(t, "recovered.example.com", claims.Recoveries[0].Hostname)

	var host models.Host
	require.NoError(t, db.First(&host, "hostname = ?", "recovered.example.com").Error)
	assert.Equal(t, models.HostAccountLimitAlertStateOK, host.AccountLimitAlertState)
}

func TestClaimDueAccountLimitAlertsSkipsSilencedHosts(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	require.NoError(t, db.Create(&models.Host{
		Hostname:                   "silenced.example.com",
		AccountCount:               95,
		AccountLimit:               100,
		Status:                     models.HostStatusActive,
		AccountLimitAlertsSilenced: true,
	}).Error)

	claims, err := r.ClaimDueAccountLimitAlerts(ctx, 0.8, time.Hour, 0)
	require.NoError(t, err)
	require.Empty(t, claims.Warnings)
	require.Empty(t, claims.Recoveries)
}

func TestUpdateHostAccountLimitAlertsSilencedResetsAlertState(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	sentAt := time.Now().Add(-time.Hour)
	host := models.Host{
		Hostname:                "silence-me.example.com",
		AccountCount:            95,
		AccountLimit:            100,
		Status:                  models.HostStatusActive,
		AccountLimitAlertState:  models.HostAccountLimitAlertStateWarning,
		AccountLimitAlertSentAt: &sentAt,
	}
	require.NoError(t, db.Create(&host).Error)

	require.NoError(t, r.UpdateHostAccountLimitAlertsSilenced(ctx, host.ID, true))

	var updated models.Host
	require.NoError(t, db.First(&updated, host.ID).Error)
	assert.True(t, updated.AccountLimitAlertsSilenced)
	assert.Equal(t, models.HostAccountLimitAlertStateOK, updated.AccountLimitAlertState)
	assert.Nil(t, updated.AccountLimitAlertSentAt)
}

func TestRecordHostAccountLimitAlertSentIgnoresStaleState(t *testing.T) {
	ctx := context.Background()
	r, db := testRelayWithHostDB(t)
	newerSentAt := time.Now()
	require.NoError(t, db.Create(&models.Host{
		Hostname:                "state.example.com",
		AccountCount:            95,
		AccountLimit:            100,
		Status:                  models.HostStatusActive,
		AccountLimitAlertState:  models.HostAccountLimitAlertStateWarning,
		AccountLimitAlertSentAt: &newerSentAt,
	}).Error)

	require.NoError(t, r.RecordHostAccountLimitAlertSent(ctx, "state.example.com", models.HostAccountLimitAlertStateOK, newerSentAt.Add(-time.Minute)))

	var host models.Host
	require.NoError(t, db.First(&host, "hostname = ?", "state.example.com").Error)
	assert.Equal(t, models.HostAccountLimitAlertStateWarning, host.AccountLimitAlertState)
	require.NotNil(t, host.AccountLimitAlertSentAt)
	assert.True(t, host.AccountLimitAlertSentAt.Equal(newerSentAt))
}

func TestListHostsApproachingAccountLimitRejectsInvalidThreshold(t *testing.T) {
	r := &Relay{}
	_, err := r.ListHostsApproachingAccountLimit(context.Background(), 0, 10)
	require.Error(t, err)
	_, err = r.ListHostsApproachingAccountLimit(context.Background(), 1.1, 10)
	require.Error(t, err)
	_, err = r.ListHostsApproachingAccountLimit(context.Background(), 0.8, -1)
	require.Error(t, err)
}
