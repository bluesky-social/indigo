package api

import "context"

type ATProto struct {
	C *XrpcClient
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
	if err := atp.C.do(ctx, Procedure, "com.atproto.createAccount", nil, body, &resp); err != nil {
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
	if err := atp.C.do(ctx, Procedure, "com.atproto.createSession", nil, body, &resp); err != nil {
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

	var out CreateRecordResponse
	if err := atp.C.do(ctx, Procedure, "com.atproto.repoCreateRecord", params, rw, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
