package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/did"
	arc "github.com/hashicorp/golang-lru/arc/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otel "go.opentelemetry.io/otel"
)

func ResolveDidToHandle(ctx context.Context, res did.Resolver, hr HandleResolver, udid string) (string, string, error) {
	ctx, span := otel.Tracer("gosky").Start(ctx, "resolveDidToHandle")
	defer span.End()

	doc, err := res.GetDocument(ctx, udid)
	if err != nil {
		return "", "", err
	}

	if len(doc.AlsoKnownAs) == 0 {
		return "", "", fmt.Errorf("users did document does not specify a handle")
	}

	aka := doc.AlsoKnownAs[0]

	u, err := url.Parse(aka)
	if err != nil {
		return "", "", fmt.Errorf("aka field in doc was not a valid url: %w", err)
	}

	handle := u.Host

	var svc *did.Service
	for _, s := range doc.Service {
		if s.ID.String() == "#atproto_pds" && s.Type == "AtprotoPersonalDataServer" {
			svc = &s
			break
		}
	}

	if svc == nil {
		return "", "", fmt.Errorf("users did document has no pds service set")
	}

	verdid, err := hr.ResolveHandleToDid(ctx, handle)
	if err != nil {
		return "", "", err
	}

	if verdid != udid {
		return "", "", fmt.Errorf("pds server reported different did for claimed handle")
	}

	return handle, svc.ServiceEndpoint, nil
}

type HandleResolver interface {
	ResolveHandleToDid(ctx context.Context, handle string) (string, error)
}

type failCacheItem struct {
	err       error
	count     int
	expiresAt time.Time
}

type ProdHandleResolver struct {
	client    *http.Client
	resolver  *net.Resolver
	ReqMod    func(*http.Request, string) error
	FailCache *arc.ARCCache[string, *failCacheItem]
}

func NewProdHandleResolver(failureCacheSize int, resolveAddr string, forceUDP bool) (*ProdHandleResolver, error) {
	failureCache, err := arc.NewARC[string, *failCacheItem](failureCacheSize)
	if err != nil {
		return nil, err
	}

	if resolveAddr == "" {
		resolveAddr = "1.1.1.1:53"
	}

	c := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   time.Second * 10,
	}

	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 10,
			}
			if forceUDP {
				network = "udp"
			}
			return d.DialContext(ctx, network, resolveAddr)
		},
	}

	return &ProdHandleResolver{
		FailCache: failureCache,
		client:    &c,
		resolver:  r,
	}, nil
}

func (dr *ProdHandleResolver) ResolveHandleToDid(ctx context.Context, handle string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*20)
	defer cancel()

	ctx, span := otel.Tracer("resolver").Start(ctx, "ResolveHandleToDid")
	defer span.End()

	var cachedFailureCount int

	if dr.FailCache != nil {
		if item, ok := dr.FailCache.Get(handle); ok {
			cachedFailureCount = item.count
			if item.expiresAt.After(time.Now()) {
				return "", item.err
			}
			dr.FailCache.Remove(handle)
		}
	}

	var wkres, dnsres string
	var wkerr, dnserr error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		wkres, wkerr = dr.resolveWellKnown(ctx, handle)
		if wkerr == nil {
			cancel()
		}
	}()
	go func() {
		defer wg.Done()
		dnsres, dnserr = dr.resolveDNS(ctx, handle)
		if dnserr == nil {
			cancel()
		}
	}()

	wg.Wait()

	if dnserr == nil {
		return dnsres, nil
	}

	if wkerr == nil {
		return wkres, nil
	}

	err := errors.Join(fmt.Errorf("no did record found for handle %q", handle), dnserr, wkerr)

	if dr.FailCache != nil {
		cachedFailureCount++
		expireAt := time.Now().Add(time.Millisecond * 100)
		if cachedFailureCount > 1 {
			// exponential backoff
			expireAt = time.Now().Add(time.Millisecond * 100 * time.Duration(cachedFailureCount*cachedFailureCount))
			// Clamp to one hour
			if expireAt.After(time.Now().Add(time.Hour)) {
				expireAt = time.Now().Add(time.Hour)
			}
		}

		dr.FailCache.Add(handle, &failCacheItem{
			err:       err,
			expiresAt: expireAt,
			count:     cachedFailureCount,
		})
	}

	return "", err
}

func (dr *ProdHandleResolver) resolveWellKnown(ctx context.Context, handle string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", err
	}

	if dr.ReqMod != nil {
		if err := dr.ReqMod(req, handle); err != nil {
			return "", err
		}
	}

	req = req.WithContext(ctx)

	resp, err := dr.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: %s", handle, err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: status=%d", handle, resp.StatusCode)
	}

	if resp.ContentLength > 2048 {
		return "", fmt.Errorf("http well-known route returned too much data")
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", fmt.Errorf("failed to read resolved did: %w", err)
	}

	parsed, err := did.ParseDID(string(b))
	if err != nil {
		return "", err
	}

	return parsed.String(), nil
}

func (dr *ProdHandleResolver) resolveDNS(ctx context.Context, handle string) (string, error) {
	res, err := dr.resolver.LookupTXT(ctx, "_atproto."+handle)
	if err != nil {
		return "", fmt.Errorf("handle lookup failed: %w", err)
	}

	for _, s := range res {
		if strings.HasPrefix(s, "did=") {
			parts := strings.Split(s, "=")
			pdid, err := did.ParseDID(parts[1])
			if err != nil {
				return "", fmt.Errorf("invalid did in record: %w", err)
			}

			return pdid.String(), nil
		}
	}

	return "", fmt.Errorf("no did record found")
}

type TestHandleResolver struct {
	TrialHosts []string
}

func (tr *TestHandleResolver) ResolveHandleToDid(ctx context.Context, handle string) (string, error) {
	c := http.DefaultClient

	for _, h := range tr.TrialHosts {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/.well-known/atproto-did", h), nil)
		if err != nil {
			return "", err
		}

		req.Host = handle

		resp, err := c.Do(req)
		if err != nil {
			slog.Warn("failed to resolve handle to DID", "handle", handle, "err", err)
			continue
		}

		if resp.StatusCode != 200 {
			slog.Warn("got non-200 status code while resolving handle", "handle", handle, "statusCode", resp.StatusCode)
			continue
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read resolved did: %w", err)
		}

		parsed, err := did.ParseDID(string(b))
		if err != nil {
			return "", err
		}

		return parsed.String(), nil
	}

	return "", fmt.Errorf("no did record found for handle %q", handle)
}
