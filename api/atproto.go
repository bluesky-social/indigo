package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/whyrusleeping/gosky/xrpc"
)

type ATProto struct {
	C *xrpc.Client
}

type TID string

type CreateAccountResp struct {
	Jwt      string `json:"jwt"`
	Username string `json:"username"`
	Did      string `json:"did"`
}

func (atp *ATProto) CreateAccount(ctx context.Context, username, email, password string) (*CreateAccountResp, error) {
	body := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}

	var resp CreateAccountResp
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.createAccount", nil, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

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

type CreateRecordResponse struct {
	Uri string `json:"url"`
	Cid string `json:"cid"`
}

func (atp *ATProto) RepoCreateRecord(ctx context.Context, did, collection string, validate bool, rec JsonLD) (*CreateRecordResponse, error) {
	params := map[string]interface{}{
		"did":        did,
		"collection": collection,
		"validate":   validate,
	}

	rw := &RecordWrapper{
		Sub: rec,
	}

	b, _ := json.Marshal(rw)
	fmt.Println(string(b))

	var out CreateRecordResponse
	if err := atp.C.Do(ctx, xrpc.Procedure, "com.atproto.repoCreateRecord", params, rw, &out); err != nil {
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
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.syncGetRepo", params, nil, &out); err != nil {
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
	if err := atp.C.Do(ctx, xrpc.Query, "com.atproto.syncGetRoot", params, nil, &out); err != nil {
		return "", err
	}

	return out.Root, nil
}
