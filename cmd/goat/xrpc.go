package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

var cmdXRPC = &cli.Command{
	Name: "xrpc",
	Usage: "use an XRPC endpoint",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "proxy",
			Usage: "the service to proxy to, referred to by its DID and service ID",
		},
		&cli.StringFlag{
			Name: "type",
			Usage: "the MIME type of the request body for a procedure",
			Value: "application/json",
		},
		// --post and --get are flags instead of subcommands so that,
		// when and if lexicon resolution is added, that can be used
		// to infer the request type
		&cli.BoolFlag{
			Name: "procedure",
			Aliases: []string{"post", "p"},
			Usage: "execute an XRPC procedure (POST request)",
		},
		&cli.BoolFlag{
			Name: "query",
			Aliases: []string{"q", "get", "g"},
			Usage: "execute an XRPC query (GET request)",
		},
	},
	ArgsUsage: "<nsid> [paramKey=paramValue...]",
	Action: runXRPC,
}

func runXRPC(cctx *cli.Context) error {
	ctx := context.Background()
	nsid := cctx.Args().First()
	if nsid == "" {
		return fmt.Errorf("need to provide NSID as argument")
	}
	
	paramList := cctx.Args().Tail()
	paramMap := make(map[string]interface{})
	for _, param := range paramList {
		split := strings.SplitN(param, "=", 2)
		if len(split) != 2 {
			return fmt.Errorf("parameters must be split with an equals sign")
		}
		if strings.Index(split[1], "\"") == 0 {
			value := strings.Trim(split[1], "\"'")
			paramMap[split[0]] = value
		} else {
			paramMap[split[0]] = split[1]
		}
	}

	procedureFlag := cctx.Bool("procedure")
	queryFlag := cctx.Bool("query")
	if !procedureFlag && !queryFlag {
		// TODO: resolve the lexicon for the provided NSID
		return fmt.Errorf("need to provide exactly one of --procedure or --query")
	} else if procedureFlag && queryFlag {
		return fmt.Errorf("need to provide exactly one of --procedure or --query")
	}

	inpenc := cctx.String("type")
	if inpenc == "" {
		inpenc = "application/json"
	}

	proxy := cctx.String("proxy")

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}
	if proxy != "" {
		if xrpcc.Headers == nil {
			xrpcc.Headers = make(map[string]string)
		}
		xrpcc.Headers["atproto-proxy"] = proxy
	}
	
	var input []byte
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		input, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("could not read input: %w", err)
		}
	}
	inputReader := bytes.NewBuffer(input)

	var rpcType xrpc.XRPCRequestType
	if procedureFlag {
		rpcType = xrpc.Procedure
	} else if queryFlag {
		rpcType = xrpc.Query
	}

	var output any
	err = xrpcc.Do(ctx, rpcType, inpenc, nsid, paramMap, inputReader, &output)
	if err != nil {
		return err
	}
	
	if reflect.TypeOf(output).Kind().String() == "map" || reflect.TypeOf(output).Kind().String() == "slice" {
		data, err := json.Marshal(output)
		if err != nil {
			return err
		}
		fmt.Println(string(data[:]))
	} else {
		fmt.Println(output)
	}
	return nil
}
