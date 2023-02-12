package api

import (
	"context"
	"fmt"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	did "github.com/whyrusleeping/go-did"
	otel "go.opentelemetry.io/otel"
)

func ResolveDidToHandle(ctx context.Context, xrpcc *xrpc.Client, pls *PLCServer, udid string) (string, string, error) {
	ctx, span := otel.Tracer("gosky").Start(ctx, "resolveDidToHandle")
	defer span.End()

	doc, err := pls.GetDocument(ctx, udid)
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
		if s.Type == "AtpPersonalDataServer" {
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

	verdid, err := comatproto.HandleResolve(ctx, xrpcc, handle)
	if err != nil {
		return "", "", err
	}

	if verdid.Did != udid {
		return "", "", fmt.Errorf("pds server reported different did for claimed handle")
	}

	return handle, svc.ServiceEndpoint, nil
}
