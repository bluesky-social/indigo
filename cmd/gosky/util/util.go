package cliutil

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/whyrusleeping/gosky/api"
	"github.com/whyrusleeping/gosky/xrpc"
)

func GetPLCClient(cctx *cli.Context) *api.PLCServer {
	h := "https://plc.directory"

	if envh := os.Getenv("BSK_PLC_URL"); envh != "" {
		h = envh
	}

	return &api.PLCServer{
		Host: h,
	}
}

func NewHttpClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func GetATPClient(cctx *cli.Context, authreq bool) (*api.ATProto, error) {
	h := "https://staging.bsky.dev"
	if pdsurl := cctx.String("pds"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cctx, authreq)
	if err != nil {
		return nil, err
	}

	return &api.ATProto{
		C: &xrpc.Client{
			Client: NewHttpClient(),
			Host:   h,
			Auth:   auth,
		},
	}, nil
}

func loadAuthFromEnv(cctx *cli.Context, req bool) (*xrpc.AuthInfo, error) {
	if a := cctx.String("auth"); a != "" {
		return ReadAuth(a)
	}

	val := os.Getenv("BSKY_AUTH")
	if val == "" {
		if req {
			return nil, fmt.Errorf("no auth env present, BSKY_AUTH not set")
		}

		return nil, nil
	}

	var auth xrpc.AuthInfo
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

func GetBskyClient(cctx *cli.Context, authreq bool) (*api.BskyApp, error) {
	h := "https://pds.staging.bsky.dev"
	if pdsurl := cctx.String("pds"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cctx, authreq)
	if err != nil {
		return nil, err
	}

	return &api.BskyApp{
		C: &xrpc.Client{
			Host: h,
			Auth: auth,
		},
	}, nil
}

func ReadAuth(fname string) (*xrpc.AuthInfo, error) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	var auth xrpc.AuthInfo
	if err := json.Unmarshal(b, &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}
