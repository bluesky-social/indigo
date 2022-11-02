package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	cli "github.com/urfave/cli/v2"
	api "github.com/whyrusleeping/gosky/api"
)

func main() {
	app := cli.NewApp()

	app.Commands = []*cli.Command{
		createSessionCmd,
		postCmd,
	}

	app.RunAndExitOnError()
}

var createSessionCmd = &cli.Command{
	Name: "createSession",
	Action: func(cctx *cli.Context) error {
		atp := &api.ATProto{
			C: &api.XrpcClient{
				Host: "https://pds.staging.bsky.dev",
			},
		}

		ses, err := atp.CreateSession(context.TODO(), cctx.Args().Get(0), cctx.Args().Get(1))
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(ses, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

func loadAuthFromEnv(req bool) (*api.AuthInfo, error) {
	val := os.Getenv("BSKY_AUTH")
	if val == "" {
		if req {
			return nil, fmt.Errorf("no auth env present, BSKY_AUTH not set")
		}

		return nil, nil
	}

	var auth api.AuthInfo
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

var postCmd = &cli.Command{
	Name: "post",
	Action: func(cctx *cli.Context) error {
		auth, err := loadAuthFromEnv(true)
		if err != nil {
			return err
		}

		atp := &api.ATProto{
			C: &api.XrpcClient{
				Host: "https://pds.staging.bsky.dev",
				Auth: auth,
			},
		}

		resp, err := atp.RepoCreateRecord(context.TODO(), auth.Did, "app.bsky.post", true, &api.PostRecord{
			Text:      strings.Join(cctx.Args().Slice(), " "),
			CreatedAt: time.Now().String(),
		})
		if err != nil {
			return err
		}

		fmt.Println(resp.Cid)
		fmt.Println(resp.Uri)

		return nil
	},
}
