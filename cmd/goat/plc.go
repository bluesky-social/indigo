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
	"github.com/bluesky-social/indigo/util"

	"github.com/did-method-plc/go-didplc"

	"github.com/urfave/cli/v2"
)

var cmdPLC = &cli.Command{
	Name:  "plc",
	Usage: "sub-commands for DID PLCs",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
	},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "history",
			Usage:     "fetch operation log for individual DID",
			ArgsUsage: `<at-identifier>`,
			Flags:     []cli.Flag{},
			Action:    runPLCHistory,
		},
		&cli.Command{
			Name:      "data",
			Usage:     "fetch current data (op) for individual DID",
			ArgsUsage: `<at-identifier>`,
			Flags:     []cli.Flag{},
			Action:    runPLCData,
		},
		&cli.Command{
			Name:  "dump",
			Usage: "output full operation log, as JSON lines",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "cursor",
					Aliases: []string{"c"},
					Usage:   "start at a given cursor offset (timestamp). use 'now' to start at current time",
				},
				&cli.BoolFlag{
					Name:    "tail",
					Aliases: []string{"f"},
					Usage:   "continue streaming PLC ops after reaching the end of log",
				},
				&cli.DurationFlag{
					Name:    "interval",
					Aliases: []string{"i"},
					Value:   3 * time.Second,
					Usage:   "sleep duration between batches for tail mode",
				},
				&cli.IntFlag{
					Name:    "batch-size",
					Aliases: []string{"s"},
					Value:   1000,
					Usage:   "batch size of operations per HTTP API request",
				},
			},
			Action: runPLCDump,
		},
		&cli.Command{
			Name:  "genesis",
			Usage: "produce an unsigned genesis operation, as JSON",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "handle",
					Usage: "atproto handle",
				},
				&cli.StringSliceFlag{
					Name:  "rotation-key",
					Usage: "rotation public key, in did:key format",
				},
				&cli.StringFlag{
					Name:  "atproto-key",
					Usage: "atproto repo signing public key, in did:key format",
				},
				&cli.StringFlag{
					Name:  "pds",
					Usage: "atproto PDS service URL",
				},
			},
			Action: runPLCGenesis,
		},
	},
}

func runPLCHistory(cctx *cli.Context) error {
	ctx := context.Background()
	plcHost := cctx.String("plc-host")
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide account identifier as an argument")
	}

	dir := identity.BaseDirectory{
		PLCURL: plcHost,
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

	url := fmt.Sprintf("%s/%s/log", plcHost, did)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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

func runPLCData(cctx *cli.Context) error {
	ctx := context.Background()
	plcHost := cctx.String("plc-host")
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide account identifier as an argument")
	}

	dir := identity.BaseDirectory{
		PLCURL: plcHost,
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

	plcData, err := fetchPLCData(ctx, plcHost, did)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(plcData, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func runPLCDump(cctx *cli.Context) error {
	ctx := context.Background()
	plcHost := cctx.String("plc-host")
	client := util.RobustHTTPClient()
	size := cctx.Int("batch-size")
	tailMode := cctx.Bool("tail")
	interval := cctx.Duration("interval")

	cursor := cctx.String("cursor")
	if cursor == "now" {
		cursor = syntax.DatetimeNow().String()
	}
	var lastCursor string

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/export", plcHost), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", *userAgent())
	q := req.URL.Query()
	q.Add("count", fmt.Sprintf("%d", size))
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
				time.Sleep(interval)
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
				time.Sleep(interval)
				continue
			}
			break
		}
		lastCursor = cursor
	}

	return nil
}

type PLCService struct {
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

type PLCData struct {
	DID                 string                `json:"did"`
	VerificationMethods map[string]string     `json:"verificationMethods"`
	RotationKeys        []string              `json:"rotationKeys"`
	AlsoKnownAs         []string              `json:"alsoKnownAs"`
	Services            map[string]PLCService `json:"services"`
}

func fetchPLCData(ctx context.Context, plcHost string, did syntax.DID) (*PLCData, error) {

	if plcHost == "" {
		return nil, fmt.Errorf("PLC host not configured")
	}

	url := fmt.Sprintf("%s/%s/data", plcHost, did)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PLC HTTP request failed")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var d PLCData
	err = json.Unmarshal(respBytes, &d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func runPLCGenesis(cctx *cli.Context) error {
	// TODO: helper function in didplc to make an empty op like this?
	services := make(map[string]didplc.OpService)
	verifMethods := make(map[string]string)
	op := didplc.RegularOp{
		Type:                "plc_operation",
		RotationKeys:        []string{},
		VerificationMethods: verifMethods,
		AlsoKnownAs:         []string{},
		Services:            services,
	}

	for _, rotationKey := range cctx.StringSlice("rotation-key") {
		if !strings.HasPrefix(rotationKey, "did:key:") {
			return fmt.Errorf("rotation keys must be in did:key format")
		}
		op.RotationKeys = append(op.RotationKeys, rotationKey)
	}

	handle := cctx.String("handle")
	if handle != "" {
		// add at:// prefix if not already present
		op.AlsoKnownAs = append(op.AlsoKnownAs, "at://"+strings.TrimPrefix(handle, "at://"))
	}

	atprotoKey := cctx.String("atproto-key")
	if atprotoKey != "" {
		if !strings.HasPrefix(atprotoKey, "did:key:") {
			return fmt.Errorf("atproto key must be in did:key format")
		}
		op.VerificationMethods["atproto"] = atprotoKey
	}

	pds := cctx.String("pds")
	if pds != "" {
		// TODO: check pds is valid URI?
		op.Services["atproto_pds"] = didplc.OpService{
			Type:     "AtprotoPersonalDataServer",
			Endpoint: pds,
		}
	}

	res, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(res))

	return nil
}
