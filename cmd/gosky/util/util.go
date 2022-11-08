package cliutil

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"github.com/whyrusleeping/gosky/api"
	"github.com/whyrusleeping/gosky/xrpc"
)

func GetPLCClient(cctx *cli.Context) *api.PLCServer {
	h := "https://plc.staging.bsky.dev"

	if envh := os.Getenv("BSK_PLC_URL"); envh != "" {
		h = envh
	}

	return &api.PLCServer{
		Host: h,
	}
}

func GetATPClient(cctx *cli.Context, authreq bool) (*api.ATProto, error) {
	h := "https://pds.staging.bsky.dev"
	if envh := os.Getenv("BSKY_PDS_URL"); envh != "" {
		h = envh
	}

	auth, err := loadAuthFromEnv(authreq)
	if err != nil {
		return nil, err
	}

	return &api.ATProto{
		C: &xrpc.Client{
			Host: h,
			Auth: auth,
		},
	}, nil
}

func loadAuthFromEnv(req bool) (*xrpc.AuthInfo, error) {
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
