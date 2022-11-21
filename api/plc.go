package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	did "github.com/whyrusleeping/go-did"
)

type PLCServer struct {
	Host string
	C    *http.Client
}

func (s *PLCServer) GetDocument(didstr string) (*did.Document, error) {
	if s.C == nil {
		s.C = http.DefaultClient
	}

	req, err := http.NewRequest("GET", s.Host+"/"+didstr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.C.Do(req)
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
