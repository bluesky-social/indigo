package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	did "github.com/whyrusleeping/go-did"
)

type PLCServer struct {
	Host string
}

func (s *PLCServer) GetDocument(didstr string) (*did.Document, error) {
	resp, err := http.Get(s.Host + "/" + didstr)
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
