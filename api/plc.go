package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

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

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get did request failed (code %d): %s", resp.StatusCode, resp.Status)
	}

	var doc did.Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

type CreateOp struct {
	Type        string  `json:"type" cborgen:"type"`
	SigningKey  string  `json:"signingKey" cborgen:"signingKey"`
	RecoveryKey string  `json:"recoveryKey" cborgen:"recoveryKey"`
	Handle      string  `json:"handle" cborgen:"handle"`
	Service     string  `json:"service" cborgen:"service"`
	Prev        *string `json:"prev" cborgen:"prev"`
	Sig         string  `json:"sig" cborgen:"sig,omitempty"`
}

func (s *PLCServer) CreateDID(ctx context.Context, sigkey *did.PrivKey, recovery string, handle string, service string) (string, error) {
	if s.C == nil {
		s.C = http.DefaultClient
	}

	op := CreateOp{
		Type:        "create",
		SigningKey:  sigkey.Public().DID(),
		RecoveryKey: recovery,
		Handle:      handle,
		Service:     service,
	}

	buf := new(bytes.Buffer)
	if err := op.MarshalCBOR(buf); err != nil {
		return "", err
	}

	sig, err := sigkey.Sign(buf.Bytes())
	if err != nil {
		return "", err
	}

	op.Sig = base64.RawURLEncoding.EncodeToString(sig)

	opdid, err := didForCreateOp(&op)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(op)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", s.Host+"/"+url.QueryEscape(opdid), bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.C.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := ioutil.ReadAll(resp.Body)
		fmt.Println(string(b))
		return "", fmt.Errorf("bad response from create call: %d %s", resp.StatusCode, resp.Status)

	}

	return opdid, nil

}

func didForCreateOp(op *CreateOp) (string, error) {
	buf := new(bytes.Buffer)
	if err := op.MarshalCBOR(buf); err != nil {
		return "", err
	}

	h := sha256.Sum256(buf.Bytes())
	enchash := base32.StdEncoding.EncodeToString(h[:])
	enchash = strings.ToLower(enchash)
	return "did:plc:" + enchash[:24], nil
}
