package relay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/util/ssrf"
)

// Simple interface for doing host and account status checks.
//
// The main reason this is an interface is to make testing/mocking easy.
type HostChecker interface {
	// host should be a URL, including scheme, hostname (and optional port), but no path segment
	CheckHost(ctx context.Context, host string) error
	FetchAccountStatus(ctx context.Context, ident *identity.Identity) (models.AccountStatus, error)
}

var _ HostChecker = (*HostClient)(nil)

type HostClient struct {
	Client    *http.Client
	UserAgent string
}

func NewHostClient(userAgent string) *HostClient {
	if userAgent == "" {
		userAgent = "indigo-relay (atproto-relay)"
	}
	c := http.Client{
		Timeout:   5 * time.Second,
		Transport: ssrf.PublicOnlyTransport(),
	}
	return &HostClient{
		Client:    &c,
		UserAgent: userAgent,
	}
}

func (hc *HostClient) apiClient(host string) *atclient.APIClient {
	client := atclient.APIClient{
		Client: hc.Client,
		Host:   host,
		Headers: map[string][]string{
			"User-Agent": []string{hc.UserAgent},
		},
	}
	return &client
}

func (hc *HostClient) CheckHost(ctx context.Context, host string) error {

	client := hc.apiClient(host)
	_, err := comatproto.ServerDescribeServer(ctx, client)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHostNotPDS, err)
	}
	return nil
}

func (hc *HostClient) FetchAccountStatus(ctx context.Context, ident *identity.Identity) (models.AccountStatus, error) {
	pdsEndpoint := ident.PDSEndpoint()
	if pdsEndpoint == "" {
		return "", fmt.Errorf("account does not declare a PDS: %s", ident.DID)
	}

	client := hc.apiClient(pdsEndpoint)

	info, err := comatproto.SyncGetRepoStatus(ctx, client, ident.DID.String())
	if err != nil {
		return "", err
	}
	if info.Active {
		return models.AccountStatusActive, nil
	} else if info.Status != nil {
		switch models.AccountStatus(*info.Status) {
		case models.AccountStatusDeactivated, models.AccountStatusDeleted, models.AccountStatusDesynchronized, models.AccountStatusSuspended, models.AccountStatusTakendown:
			return models.AccountStatus(*info.Status), nil
		default:
			return models.AccountStatusInactive, nil
		}
	} else {
		return models.AccountStatusInactive, nil
	}
}

type MockHostChecker struct {
	Hosts    map[string]bool
	Accounts map[string]models.AccountStatus
}

func NewMockHostChecker() *MockHostChecker {
	return &MockHostChecker{
		Hosts:    make(map[string]bool),
		Accounts: make(map[string]models.AccountStatus),
	}
}

func (hc *MockHostChecker) CheckHost(ctx context.Context, host string) error {
	_, ok := hc.Hosts[host]
	if !ok {
		return ErrHostNotPDS
	}
	return nil
}

func (hc *MockHostChecker) FetchAccountStatus(ctx context.Context, ident *identity.Identity) (models.AccountStatus, error) {
	status, ok := hc.Accounts[ident.DID.String()]
	if !ok {
		return "", ErrAccountNotFound
	}
	return status, nil
}
