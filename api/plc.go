package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	did "github.com/whyrusleeping/go-did"
	otel "go.opentelemetry.io/otel"
)

type PLCServer struct {
	Host string
	C    *http.Client
}

func (s *PLCServer) GetDocument(ctx context.Context, didstr string) (*did.Document, error) {
	ctx, span := otel.Tracer("gosky").Start(ctx, "plsResolveDid")
	defer span.End()

	if s.C == nil {
		s.C = http.DefaultClient
	}

	req, err := http.NewRequest("GET", s.Host+"/"+didstr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.C.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get did request failed (code %d): %s", resp.StatusCode, resp.Status)
	}

	var doc did.Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	return &doc, nil
}
