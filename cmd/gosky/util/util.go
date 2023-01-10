package cliutil

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"github.com/whyrusleeping/gosky/api"
	"github.com/whyrusleeping/gosky/xrpc"
)

func GetPLCClient(cctx *cli.Context) *api.PLCServer {
	return &api.PLCServer{
		Host: cctx.String("plc"),
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

type CliConfig struct {
	filename string
	PDS      string
}

func readGoskyConfig() (*CliConfig, error) {
	d, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("cannot read Home directory")
	}

	f := filepath.Join(d, ".gosky")

	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, nil
	}

	var out CliConfig
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	out.filename = f
	return &out, nil
}

var Config *CliConfig

func TryReadConfig() {
	cfg, err := readGoskyConfig()
	if err != nil {
		fmt.Println(err)
	} else {
		Config = cfg
	}
}

func WriteConfig(cfg *CliConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(cfg.filename, b, 0664)
}

func GetATPClient(cctx *cli.Context, authreq bool) (*api.ATProto, error) {
	h := "https://bsky.social"
	if pdsurl := cctx.String("pds"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cctx, authreq)
	if err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
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
		if ai, err := ReadAuth(a); err != nil && req {
			return nil, err
		} else {
			return ai, nil
		}
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
