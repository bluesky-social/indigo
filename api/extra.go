package api

import (
	"context"
	"fmt"
	"net/url"

	did "github.com/whyrusleeping/go-did"
)

func ResolveDidToHandle(ctx context.Context, atp *ATProto, pls *PLCServer, udid string) (string, string, error) {
	doc, err := pls.GetDocument(udid)
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

	if svc.ServiceEndpoint != atp.C.Host {
		return "", "", fmt.Errorf("our atp client is authed for a different pds (%s != %s)", svc.ServiceEndpoint, atp.C.Host)
	}

	verdid, err := atp.HandleResolve(ctx, handle)
	if err != nil {
		return "", "", err
	}

	if verdid != udid {
		return "", "", fmt.Errorf("pds server reported different did for claimed handle")
	}

	return handle, svc.ServiceEndpoint, nil
}
