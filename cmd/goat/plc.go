package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

var cmdPLC = &cli.Command{
	Name:  "plc",
	Usage: "sub-commands for DID PLCs",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "plc-directory",
			Value: "https://plc.directory",
		},
	},
	Subcommands: []*cli.Command{
		cmdPLCHistory,
		cmdPLCDump,
	},
}

var cmdPLCHistory = &cli.Command{
	Name:      "history",
	Usage:     "fetch operation log for individual DID",
	ArgsUsage: `<at-identifier>`,
	Flags:     []cli.Flag{},
	Action:    runPLCHistory,
}

func runPLCHistory(cctx *cli.Context) error {
	ctx := context.Background()
	plcURL := cctx.String("plc-directory")
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide account identifier as an argument")
	}

	dir := identity.BaseDirectory{
		PLCURL: plcURL,
	}

	id, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}
	var did syntax.DID
	if id.IsDID() {
		did, err = id.AsDID()
		if err != nil {
			return err
		}
	} else {
		hdl, err := id.AsHandle()
		if err != nil {
			return err
		}
		did, err = dir.ResolveHandle(ctx, hdl)
		if err != nil {
			return err
		}
	}

	if did.Method() != "plc" {
		return fmt.Errorf("non-PLC DID method: %s", did.Method())
	}

	url := fmt.Sprintf("%s/%s/log", plcURL, did)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PLC HTTP request failed")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// parse JSON and reformat for printing
	var oplog []map[string]interface{}
	err = json.Unmarshal(respBytes, &oplog)
	if err != nil {
		return err
	}

	for _, op := range oplog {
		b, err := json.MarshalIndent(op, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}

	return nil
}

var cmdPLCDump = &cli.Command{
	Name:  "dump",
	Usage: "output full operation log, as JSON lines",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "cursor",
		},
		&cli.BoolFlag{
			Name: "tail",
		},
	},
	Action: runPLCDump,
}

func runPLCDump(cctx *cli.Context) error {
	ctx := context.Background()
	plcURL := cctx.String("plc-directory")
	client := http.DefaultClient
	tailMode := cctx.Bool("tail")

	cursor := cctx.String("cursor")
	if cursor == "now" {
		cursor = syntax.DatetimeNow().String()
	}
	var lastCursor string

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/export", plcURL), nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Add("count", "1000")
	req.URL.RawQuery = q.Encode()

	for {
		q := req.URL.Query()
		if cursor != "" {
			q.Set("after", cursor)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("PLC HTTP request failed status=%d", resp.StatusCode)
		}
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		lines := strings.Split(string(respBytes), "\n")
		if len(lines) == 0 || (len(lines) == 1 && len(lines[0]) == 0) {
			if tailMode {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
		for _, l := range lines {
			if len(l) < 2 {
				break
			}
			var op map[string]interface{}
			err = json.Unmarshal([]byte(l), &op)
			if err != nil {
				return err
			}
			var ok bool
			cursor, ok = op["createdAt"].(string)
			if !ok {
				return fmt.Errorf("missing createdAt in PLC op log")
			}
			if cursor == lastCursor {
				continue
			}

			b, err := json.Marshal(op)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		}
		if cursor != "" && cursor == lastCursor {
			if tailMode {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
		lastCursor = cursor
	}

	return nil
}
