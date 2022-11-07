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
	"github.com/whyrusleeping/gosky/xrpc"
)

func main() {
	app := cli.NewApp()

	app.Commands = []*cli.Command{
		createSessionCmd,
		postCmd,
		didCmd,
		syncCmd,
	}

	app.RunAndExitOnError()
}

var createSessionCmd = &cli.Command{
	Name: "createSession",
	Action: func(cctx *cli.Context) error {
		atp := &api.ATProto{
			C: &xrpc.Client{
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

var postCmd = &cli.Command{
	Name: "post",
	Action: func(cctx *cli.Context) error {
		auth, err := loadAuthFromEnv(true)
		if err != nil {
			return err
		}

		atp := &api.ATProto{
			C: &xrpc.Client{
				Host: "https://pds.staging.bsky.dev",
				Auth: auth,
			},
		}

		text := strings.Join(cctx.Args().Slice(), " ")

		resp, err := atp.RepoCreateRecord(context.TODO(), auth.Did, "app.bsky.post", true, &api.PostRecord{
			Text:      text,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}

		fmt.Println(resp.Cid)
		fmt.Println(resp.Uri)

		return nil
	},
}

var didCmd = &cli.Command{
	Name: "did",
	Subcommands: []*cli.Command{
		didGetCmd,
	},
}

var didGetCmd = &cli.Command{
	Name: "get",
	Action: func(cctx *cli.Context) error {
		s := &api.PLCServer{
			Host: "https://plc.staging.bsky.dev",
		}

		doc, err := s.GetDocument(cctx.Args().First())
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var syncCmd = &cli.Command{
	Name: "sync",
	Subcommands: []*cli.Command{
		syncGetRepoCmd,
		syncGetRootCmd,
	},
}

var syncGetRepoCmd = &cli.Command{
	Name: "getRepo",
	Action: func(cctx *cli.Context) error {
		atp := &api.ATProto{
			C: &xrpc.Client{
				Host: "https://pds.staging.bsky.dev",
			},
		}

		ctx := context.TODO()

		repobytes, err := atp.SyncGetRepo(ctx, cctx.Args().First(), nil)
		if err != nil {
			return err
		}

		fmt.Printf("%x", repobytes)

		return nil
	},
}

var syncGetRootCmd = &cli.Command{
	Name: "getRoot",
	Action: func(cctx *cli.Context) error {
		atp := &api.ATProto{
			C: &xrpc.Client{
				Host: "https://pds.staging.bsky.dev",
			},
		}

		ctx := context.TODO()

		root, err := atp.SyncGetRoot(ctx, cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println(root)

		return nil
	},
}
