package relay

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListHostsApproachingAccountLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Host{}))

	hosts := []models.Host{
		{Hostname: "below.example.com", AccountCount: 79, AccountLimit: 100, Status: models.HostStatusActive},
		{Hostname: "at.example.com", AccountCount: 80, AccountLimit: 100, Status: models.HostStatusActive},
		{Hostname: "over.example.com", AccountCount: 45, AccountLimit: 50, Status: models.HostStatusActive},
		{Hostname: "banned.example.com", AccountCount: 100, AccountLimit: 100, Status: models.HostStatusBanned},
	}
	for _, host := range hosts {
		require.NoError(t, db.Create(&host).Error)
	}

	r := &Relay{db: db}
	usages, err := r.ListHostsApproachingAccountLimit(context.Background(), 0.80, 10)
	require.NoError(t, err)
	require.Len(t, usages, 2)

	assert.Equal(t, "over.example.com", usages[0].Hostname)
	assert.Equal(t, int64(45), usages[0].AccountCount)
	assert.Equal(t, int64(50), usages[0].AccountLimit)
	assert.Equal(t, 0.90, usages[0].Usage)
	assert.Equal(t, "at.example.com", usages[1].Hostname)
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
