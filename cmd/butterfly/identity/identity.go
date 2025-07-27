package identity

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/time/rate"

	"github.com/bluesky-social/indigo/cmd/butterfly/store"
)

// IdentityResolver wraps the atproto identity directory with butterfly-specific configuration
type IdentityResolver struct {
	directory identity.Directory
}

// IdentityResolverConfig contains configuration options for identity resolution
type IdentityResolverConfig struct {
	// URL for PLC directory (defaults to https://plc.directory)
	PLCURL string
	// Store for caching
	Store store.Store
	// Cache TTL for successful lookups
	CacheTTL time.Duration
	// HTTP client timeout
	HTTPTimeout time.Duration
	// User agent string
	UserAgent string
	// Rate limit for PLC requests (requests per second)
	PLCRateLimit float64
	// Enable authoritative DNS fallback
	TryAuthoritativeDNS bool
}

// NewIdentityResolver creates a new identity resolver with custom configuration
func NewIdentityResolver(config IdentityResolverConfig) *IdentityResolver {
	// Set defaults
	if config.PLCURL == "" {
		config.PLCURL = "https://plc.directory"
	}
	if config.HTTPTimeout == 0 {
		config.HTTPTimeout = 30 * time.Second
	}
	if config.UserAgent == "" {
		config.UserAgent = "butterfly/0.0.1"
	}
	if config.PLCRateLimit == 0 {
		config.PLCRateLimit = 10 // 10 requests per second default
	}

	// Create base directory
	baseDir := &identity.BaseDirectory{
		PLCURL:     config.PLCURL,
		PLCLimiter: rate.NewLimiter(rate.Limit(config.PLCRateLimit), 1),
		HTTPClient: http.Client{
			Timeout: config.HTTPTimeout,
		},
		UserAgent:           config.UserAgent,
		TryAuthoritativeDNS: config.TryAuthoritativeDNS,
	}

	// Wrap with store caching
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}
	cacheDir := NewStoreDirectory(baseDir, config.Store, config.CacheTTL, time.Minute, 5*time.Minute)

	return &IdentityResolver{
		directory: &cacheDir,
	}
}

// ResolveHandle looks up a handle and returns the associated identity
func (r *IdentityResolver) ResolveHandle(ctx context.Context, handle string) (*identity.Identity, error) {
	h, err := syntax.ParseHandle(handle)
	if err != nil {
		return nil, fmt.Errorf("invalid handle %q: %w", handle, err)
	}

	ident, err := r.directory.LookupHandle(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve handle %q: %w", handle, err)
	}

	return ident, nil
}

// ResolveDID looks up a DID and returns the associated identity
func (r *IdentityResolver) ResolveDID(ctx context.Context, did string) (*identity.Identity, error) {
	d, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("invalid DID %q: %w", did, err)
	}

	ident, err := r.directory.LookupDID(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve DID %q: %w", did, err)
	}

	return ident, nil
}

// Resolve looks up either a handle or DID and returns the associated identity
func (r *IdentityResolver) Resolve(ctx context.Context, identifier string) (*identity.Identity, error) {
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return nil, fmt.Errorf("invalid identifier %q: %w", identifier, err)
	}

	ident, err := r.directory.Lookup(ctx, *atid)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve identifier %q: %w", identifier, err)
	}

	return ident, nil
}

// GetPDSEndpoint returns the PDS endpoint for a given identity
func (r *IdentityResolver) GetPDSEndpoint(ident *identity.Identity) (string, error) {
	if ident == nil {
		return "", fmt.Errorf("identity is nil")
	}

	// Look for the atproto_pds service endpoint
	if svc, ok := ident.Services["atproto_pds"]; ok {
		return svc.URL, nil
	}

	return "", fmt.Errorf("no PDS endpoint found for DID %s", ident.DID)
}

// GetPublicKey returns the public key for a given identity and key ID
func (r *IdentityResolver) GetPublicKey(ident *identity.Identity, keyID string) (*identity.VerificationMethod, error) {
	if ident == nil {
		return nil, fmt.Errorf("identity is nil")
	}

	if key, ok := ident.Keys[keyID]; ok {
		return &key, nil
	}

	return nil, fmt.Errorf("key %q not found for DID %s", keyID, ident.DID)
}

// GetSigningKey returns the primary signing key for a given identity
func (r *IdentityResolver) GetSigningKey(ident *identity.Identity) (*identity.VerificationMethod, error) {
	// Try the atproto signing key first
	if key, err := r.GetPublicKey(ident, "atproto"); err == nil {
		return key, nil
	}

	// Fall back to the first available key
	for _, key := range ident.Keys {
		return &key, nil
	}

	return nil, fmt.Errorf("no signing key found for DID %s", ident.DID)
}

// Purge removes an identifier from the cache (if caching is enabled)
func (r *IdentityResolver) Purge(ctx context.Context, identifier string) error {
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return fmt.Errorf("invalid identifier %q: %w", identifier, err)
	}

	return r.directory.Purge(ctx, *atid)
}
