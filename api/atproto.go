package api

import (
	"bytes"
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

type ATProto struct {
	C *xrpc.Client
}

type TID string

type CreateSessionResp struct {
	Jwt      string `json:"jwt"`
	Username string `json:"name"` // TODO: probably will change to username
	Did      string `json:"did"`
}

func (atp *ATProto) CreateSession(ctx context.Context, username, password string) (*CreateSessionResp, error) {
	body := map[string]string{
		"username": username,
		"password": password,
	}

	var resp CreateSessionResp
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.session.create", nil, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

type CreateAccountResp struct {
	AccessJwt      string `json:"accessJwt"`
	RefreshJwt     string `json:"refreshJwt"`
	Handle         string `json:"handle"`
	Did            string `json:"did"`
	DeclarationCid string `json:"declarationCid"`
}

func (atp *ATProto) CreateAccount(ctx context.Context, email, handle, password string) (*CreateAccountResp, error) {
	body := map[string]string{
		"email":    email,
		"handle":   handle,
		"password": password,
	}

	var resp CreateAccountResp
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.account.create", nil, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

type CreateRecordResponse struct {
	Uri string `json:"url"`
	Cid string `json:"cid"`
}

func (atp *ATProto) RepoCreateRecord(ctx context.Context, did, collection string, validate bool, rec JsonLD) (*CreateRecordResponse, error) {
	rw := &RecordWrapper{
		Sub: rec,
	}

	body := map[string]interface{}{
		"did":        did,
		"collection": collection,
		"validate":   validate,
		"record":     rw,
	}

	var out CreateRecordResponse
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.repo.createRecord", nil, body, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

func (atp *ATProto) SyncGetRepo(ctx context.Context, did string, from *string) ([]byte, error) {
	params := map[string]interface{}{
		"did": did,
	}
	if from != nil {
		params["from"] = *from
	}

	out := new(bytes.Buffer)
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.sync.getRepo", params, nil, out); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

func (atp *ATProto) SyncGetRoot(ctx context.Context, did string) (string, error) {
	params := map[string]interface{}{
		"did": did,
	}

	var out struct {
		Root string `json:"root"`
	}
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.sync.getRoot", params, nil, &out); err != nil {
		return "", err
	}

	return out.Root, nil
}
