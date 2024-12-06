package fakedata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/carlmjohnson/versioninfo"
)

type AccountCatalog struct {
	Celebs   []AccountContext
	Regulars []AccountContext
}

func (ac *AccountCatalog) Combined() []AccountContext {
	var combined []AccountContext
	combined = append(combined, ac.Celebs...)
	combined = append(combined, ac.Regulars...)
	return combined
}

type AccountContext struct {
	// 0-based index; should match index
	Index       int           `json:"index"`
	AccountType string        `json:"accountType"`
	Email       string        `json:"email"`
	Password    string        `json:"password"`
	Auth        xrpc.AuthInfo `json:"auth"`
}

func ReadAccountCatalog(path string) (*AccountCatalog, error) {
	catalog := &AccountCatalog{}
	catFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer catFile.Close()

	decoder := json.NewDecoder(catFile)
	for decoder.More() {
		var usr AccountContext
		if err := decoder.Decode(&usr); err != nil {
			return nil, fmt.Errorf("parse AccountContext: %w", err)
		}
		switch usr.AccountType {
		case "celebrity":
			catalog.Celebs = append(catalog.Celebs, usr)
		case "regular":
			catalog.Regulars = append(catalog.Regulars, usr)
		default:
			return nil, fmt.Errorf("unhandled account type: %v", usr.AccountType)
		}
	}
	// validate index numbers
	for i, u := range catalog.Celebs {
		if i != u.Index {
			return nil, fmt.Errorf("account index didn't match: %d != %d (%s)", i, u.Index, u.AccountType)
		}
	}
	for i, u := range catalog.Regulars {
		if i != u.Index {
			return nil, fmt.Errorf("account index didn't match: %d != %d (%s)", i, u.Index, u.AccountType)
		}
	}
	log.Info("loaded account catalog", "regular", len(catalog.Regulars), "celebrity", len(catalog.Celebs))
	return catalog, nil
}

func AccountXrpcClient(pdsHost string, ac *AccountContext) (*xrpc.Client, error) {
	httpClient := util.RobustHTTPClient()
	ua := "IndigoFakerMaker/" + versioninfo.Short()
	xrpcc := &xrpc.Client{
		Client:    httpClient,
		Host:      pdsHost,
		Auth:      &ac.Auth,
		UserAgent: &ua,
	}
	// use XRPC client to re-auth using user/pass
	auth, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
		Identifier: ac.Auth.Handle,
		Password:   ac.Password,
	})
	if err != nil {
		return nil, err
	}
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt
	return xrpcc, nil
}
