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
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

func (atp *ATProto) CreateSession(ctx context.Context, handle, password string) (*CreateSessionResp, error) {
	body := map[string]string{
		"handle":   handle,
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

func (atp *ATProto) CreateAccount(ctx context.Context, email, handle, password string, invite *string) (*CreateAccountResp, error) {
	body := map[string]string{
		"email":    email,
		"handle":   handle,
		"password": password,
	}

	if invite != nil {
		body["inviteCode"] = *invite
	}

	var resp CreateAccountResp
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.account.create", nil, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

type CreateRecordResponse struct {
	Uri string `json:"uri"`
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

func (atp *ATProto) HandleResolve(ctx context.Context, handle string) (string, error) {
	params := map[string]interface{}{
		"handle": handle,
	}

	var out struct {
		Did string `json:"did"`
	}
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.handle.resolve", params, nil, &out); err != nil {
		return "", err
	}

	return out.Did, nil

}

func (atp *ATProto) SessionRefresh(ctx context.Context) (*xrpc.AuthInfo, error) {
	var out xrpc.AuthInfo
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.session.refresh", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

type RecordResponse[T JsonLD] struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
	Value T `json:"value"`

}

func RepoGetRecord[T JsonLD](atp *ATProto, ctx context.Context, user string, collection string, rkey string) (*RecordResponse[T], error) {
	params := map[string]interface{}{
		"user":       user,
		"collection": collection,
		"rkey":       rkey,
	}

	var out RecordResponse[T]
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.repo.getRecord", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
