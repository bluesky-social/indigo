package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
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
			Usage: "produce an unsigned genesis operation",
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
		&cli.Command{
			Name:      "calc-did",
			Usage:     "calculate the DID corresponding to a signed PLC operation",
			ArgsUsage: `<signed_genesis.json>`,
			Flags:     []cli.Flag{},
			Action:    runPLCCalcDID,
		},
		&cli.Command{
			Name:      "sign",
			Usage:     "sign an operation, ready to be submitted",
			ArgsUsage: `<operation.json>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "plc-signing-key",
					Usage:   "private key used to sign operation (multibase syntax)",
					EnvVars: []string{"PLC_SIGNING_KEY"},
				},
			},
			Action: runPLCSign,
		},
		&cli.Command{
			Name:      "submit",
			Usage:     "submit a signed operation to the PLC directory",
			ArgsUsage: `<signed_operation.json>`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "genesis",
					Usage: "the operation is a genesis operation",
				},
				&cli.StringFlag{
					Name:  "did",
					Usage: "the DID of the identity to update",
				},
			},
			Action: runPLCSubmit,
		},
		&cli.Command{
			Name:      "update",
			Usage:     "apply updates to a previous operation to produce a new one (but don't sign or submit it, yet)",
			ArgsUsage: `<DID>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "prev",
					Usage: "the CID of the operation to use as a base (uses most recent op if not specified)",
				},
				&cli.StringFlag{
					Name:  "handle",
					Usage: "atproto handle",
				},
				&cli.StringSliceFlag{
					Name:  "add-rotation-key",
					Usage: "rotation public key, in did:key format (added to front of rotationKey list)",
				},
				&cli.StringSliceFlag{
					Name:  "remove-rotation-key",
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
			Action: runPLCUpdate,
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
		if _, err := crypto.ParsePublicDIDKey(rotationKey); err != nil {
			return err
		}
		op.RotationKeys = append(op.RotationKeys, rotationKey)
	}

	handle := cctx.String("handle")
	if handle != "" {
		parsedHandle, err := syntax.ParseHandle(strings.TrimPrefix(handle, "at://"))
		if err != nil {
			return err
		}
		parsedHandle = parsedHandle.Normalize()
		op.AlsoKnownAs = append(op.AlsoKnownAs, "at://"+string(parsedHandle))
	}

	atprotoKey := cctx.String("atproto-key")
	if atprotoKey != "" {
		if _, err := crypto.ParsePublicDIDKey(atprotoKey); err != nil {
			return err
		}
		op.VerificationMethods["atproto"] = atprotoKey
	}

	pds := cctx.String("pds")
	if pds != "" {
		parsedUrl, err := url.Parse(pds)
		if err != nil {
			return err
		}
		if !parsedUrl.IsAbs() {
			return fmt.Errorf("invalid PDS URL: must be absolute")
		}
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

func runPLCCalcDID(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide genesis json path as input")
	}

	inputReader, err := getFileOrStdin(s)
	if err != nil {
		return err
	}

	inBytes, err := io.ReadAll(inputReader)
	if err != nil {
		return err
	}

	var enum didplc.OpEnum
	if err := json.Unmarshal(inBytes, &enum); err != nil {
		return err
	}
	op := enum.AsOperation()

	did, err := op.DID() // errors if op is not a signed genesis op
	if err != nil {
		return err
	}

	fmt.Println(did)

	return nil
}

func runPLCSign(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide PLC operation json path as input")
	}

	privStr := cctx.String("plc-signing-key")
	if privStr == "" {
		return fmt.Errorf("private key must be provided (HINT: use `goat account plc` if your PDS holds the keys)")
	}

	inputReader, err := getFileOrStdin(s)
	if err != nil {
		return err
	}

	inBytes, err := io.ReadAll(inputReader)
	if err != nil {
		return err
	}

	var enum didplc.OpEnum
	if err := json.Unmarshal(inBytes, &enum); err != nil {
		return err
	}
	op := enum.AsOperation()

	// Note: we do not require that the op is currently unsigned.
	// If it's already signed, we'll re-sign it.

	privkey, err := crypto.ParsePrivateMultibase(privStr)
	if err != nil {
		return err
	}

	if err := op.Sign(privkey); err != nil {
		return err
	}

	res, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(res))

	return nil
}

func runPLCSubmit(cctx *cli.Context) error {
	ctx := context.Background()
	expectGenesis := cctx.Bool("genesis")
	didString := cctx.String("did")

	if !expectGenesis && didString == "" {
		return fmt.Errorf("exactly one of either --genesis or --did must be specified")
	}

	if expectGenesis && didString != "" {
		return fmt.Errorf("exactly one of either --genesis or --did must be specified")
	}

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide PLC operation json path as input")
	}

	inputReader, err := getFileOrStdin(s)
	if err != nil {
		return err
	}

	inBytes, err := io.ReadAll(inputReader)
	if err != nil {
		return err
	}

	var enum didplc.OpEnum
	if err := json.Unmarshal(inBytes, &enum); err != nil {
		return fmt.Errorf("failed decoding PLC op JSON: %w", err)
	}
	op := enum.AsOperation()

	if op.IsGenesis() != expectGenesis {
		if expectGenesis {
			return fmt.Errorf("expected genesis operation, but a non-genesis operation was provided")
		} else {
			return fmt.Errorf("expected non-genesis operation, but a genesis operation was provided")
		}
	}

	if op.IsGenesis() {
		didString, err = op.DID()
		if err != nil {
			return err
		}
	}

	if !op.IsSigned() {
		return fmt.Errorf("operation must be signed")
	}

	c := didplc.Client{
		DirectoryURL: cctx.String("plc-host"),
		UserAgent:    *userAgent(),
	}

	if err = c.Submit(ctx, didString, op); err != nil {
		return err
	}

	fmt.Println("success")

	return nil
}

// fetch logs from /log/audit, select according to base_cid ("" means use latest), and
// prepare it for updates:
//   - convert from legacy op format if needed (and reject tombstone ops)
//   - strip signature
//   - set `prev` to appropriate value
func fetchOpForUpdate(ctx context.Context, c didplc.Client, did string, base_cid string) (*didplc.RegularOp, error) {
	auditlog, err := c.AuditLog(ctx, did)
	if err != nil {
		return nil, err
	}

	if err = didplc.VerifyOpLog(auditlog); err != nil {
		return nil, err
	}

	var baseLogEntry *didplc.LogEntry
	if base_cid == "" {
		// use most recent entry
		baseLogEntry = &auditlog[len(auditlog)-1]
	} else {
		// scan for the specified entry
		for _, entry := range auditlog {
			if entry.CID == base_cid {
				baseLogEntry = &entry
				break
			}
		}
		if baseLogEntry == nil {
			return nil, fmt.Errorf("no operation found matching CID %s", base_cid)
		}
	}
	var op didplc.RegularOp
	switch baseOp := baseLogEntry.Operation.AsOperation().(type) {
	case *didplc.RegularOp:
		op = *baseOp
		op.Sig = nil
	case *didplc.LegacyOp:
		op = baseOp.RegularOp() // also strips sig
	case *didplc.TombstoneOp:
		return nil, fmt.Errorf("cannot update from a tombstone op")
	}
	op.Prev = &baseLogEntry.CID
	return &op, nil
}

func runPLCUpdate(cctx *cli.Context) error {
	ctx := context.Background()
	prevCID := cctx.String("prev")

	didString := cctx.Args().First()
	if didString == "" {
		return fmt.Errorf("please specify a DID to update")
	}

	c := didplc.Client{
		DirectoryURL: cctx.String("plc-host"),
		UserAgent:    *userAgent(),
	}
	op, err := fetchOpForUpdate(ctx, c, didString, prevCID)
	if err != nil {
		return err
	}

	for _, rotationKey := range cctx.StringSlice("remove-rotation-key") {
		if _, err := crypto.ParsePublicDIDKey(rotationKey); err != nil {
			return err
		}
		removeSuccess := false
		for idx, existingRotationKey := range op.RotationKeys {
			if existingRotationKey == rotationKey {
				op.RotationKeys = append(op.RotationKeys[:idx], op.RotationKeys[idx+1:]...)
				removeSuccess = true
			}
		}
		if !removeSuccess {
			return fmt.Errorf("failed remove rotation key %s, not found in array", rotationKey)
		}
	}

	for _, rotationKey := range cctx.StringSlice("add-rotation-key") {
		if _, err := crypto.ParsePublicDIDKey(rotationKey); err != nil {
			return err
		}
		// prepend (Note: if adding multiple rotation keys at once, they'll end up in reverse order)
		op.RotationKeys = append([]string{rotationKey}, op.RotationKeys...)
	}

	handle := cctx.String("handle")
	if handle != "" {
		parsedHandle, err := syntax.ParseHandle(strings.TrimPrefix(handle, "at://"))
		if err != nil {
			return err
		}

		// strip any existing at:// akas
		// (someone might have some non-atproto akas, we will leave them untouched,
		// they can manually manage those or use some other tool if needed)
		var akas []string
		for _, aka := range op.AlsoKnownAs {
			if !strings.HasPrefix(aka, "at://") {
				akas = append(akas, aka)
			}
		}
		op.AlsoKnownAs = append(akas, "at://"+string(parsedHandle))
	}

	atprotoKey := cctx.String("atproto-key")
	if atprotoKey != "" {
		if _, err := crypto.ParsePublicDIDKey(atprotoKey); err != nil {
			return err
		}
		op.VerificationMethods["atproto"] = atprotoKey
	}

	pds := cctx.String("pds")
	if pds != "" {
		parsedUrl, err := url.Parse(pds)
		if err != nil {
			return err
		}
		if !parsedUrl.IsAbs() {
			return fmt.Errorf("invalid PDS URL: must be absolute")
		}
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
