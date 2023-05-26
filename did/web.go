package did

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/whyrusleeping/go-did"
)

type WebResolver struct {
	// TODO: cache? maybe at a different layer
}

func (wr *WebResolver) GetDocument(ctx context.Context, didstr string) (*Document, error) {
	pdid, err := did.ParseDID(didstr)
	if err != nil {
		return nil, err
	}

	val := pdid.Value()
	if strings.Contains(val, ":") {
		return nil, fmt.Errorf("did:web resolver does not handle ports or documents at sub-paths")
	}

	resp, err := http.Get("https://" + val + "/.well-known/did.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch did request failed (status %d): %s", resp.StatusCode, resp.Status)
	}

	var out did.Document
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return &out, nil
}
