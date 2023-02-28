package api

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

type ATProto struct {
	C *xrpc.Client
}

const (
	encJson = "application/json"
)

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
	if err := atp.C.Do(ctx, xrpc.Procedure, encJson, "com.atproto.account.create", nil, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}
