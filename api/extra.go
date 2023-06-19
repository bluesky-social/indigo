package api

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/xrpc"
	logging "github.com/ipfs/go-log"
	otel "go.opentelemetry.io/otel"
)

var log = logging.Logger("api")

func ResolveDidToHandle(ctx context.Context, xrpcc *xrpc.Client, res did.Resolver, hr HandleResolver, udid string) (string, string, error) {
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

	if svc.ServiceEndpoint != xrpcc.Host {
		return "", "", fmt.Errorf("our XRPC client is authed for a different pds (%s != %s)", svc.ServiceEndpoint, xrpcc.Host)
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

type ProdHandleResolver struct {
}

func (dr *ProdHandleResolver) ResolveHandleToDid(ctx context.Context, handle string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	c := http.DefaultClient

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", err
	}

	req = req.WithContext(ctx)

	resp, wkerr := c.Do(req)
	if wkerr == nil && resp.StatusCode == 200 {
		if resp.ContentLength > 2048 {
			return "", fmt.Errorf("http well-known route returned too much data")
		}

		b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 2048))
		if err != nil {
			return "", fmt.Errorf("failed to read resolved did: %w", err)
		}

		parsed, err := did.ParseDID(string(b))
		if err != nil {
			return "", err
		}

		return parsed.String(), nil
	}
	log.Infof("failed to resolve handle (%s) through HTTP well-known route: %s", handle, wkerr)

	res, err := net.LookupTXT("_atproto." + handle)
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

	return "", fmt.Errorf("no did record found for handle %q", handle)
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
			log.Warnf("failed to get did: %s", err)
			continue
		}

		if resp.StatusCode != 200 {
			log.Warnf("got non-200 status code while fetching did: %d", resp.StatusCode)
			continue
		}

		b, err := ioutil.ReadAll(resp.Body)
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
