package main

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

var cmdSyntax = &cli.Command{
	Name:  "syntax",
	Usage: "sub-commands for string syntax helpers",
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:  "tid",
			Usage: "sub-commands for TIDs",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates TID syntax",
					ArgsUsage: `<tid>`,
					Action:    runSyntaxTIDCheck,
				},
				&cli.Command{
					Name:      "inspect",
					Usage:     "parses a TID to timestamp",
					ArgsUsage: `<tid>`,
					Action:    runSyntaxTIDInspect,
				},
				&cli.Command{
					Name:    "generate",
					Usage:   "outputs a new TID",
					Aliases: []string{"now"},
					Action:  runSyntaxTIDGenerate,
				},
			},
		},
		&cli.Command{
			Name:  "handle",
			Usage: "sub-commands for handle syntax",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates handle syntax",
					ArgsUsage: `<handle>`,
					Action:    runSyntaxHandleCheck,
				},
			},
		},
		&cli.Command{
			Name:  "did",
			Usage: "sub-commands for DID syntax",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates DID syntax",
					ArgsUsage: `<did>`,
					Action:    runSyntaxDIDCheck,
				},
			},
		},
		&cli.Command{
			Name:  "rkey",
			Usage: "sub-commands for record key syntax",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates record key syntax",
					ArgsUsage: `<rkey>`,
					Action:    runSyntaxRecordKeyCheck,
				},
			},
		},
		&cli.Command{
			Name:  "nsid",
			Usage: "sub-commands for NSID syntax",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates NSID syntax",
					ArgsUsage: `<nsid>`,
					Action:    runSyntaxNSIDCheck,
				},
			},
		},
		&cli.Command{
			Name:  "at-uri",
			Usage: "sub-commands for AT-URI syntax",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "check",
					Usage:     "validates AT-URI syntax",
					ArgsUsage: `<uri>`,
					Action:    runSyntaxATURICheck,
				},
			},
		},
	},
}

func runSyntaxTIDCheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseTID(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

func runSyntaxTIDGenerate(cctx *cli.Context) error {
	fmt.Printf("%s\n", syntax.NewTIDNow(0).String())
	return nil
}

func runSyntaxTIDInspect(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	tid, err := syntax.ParseTID(s)
	if err != nil {
		return err
	}
	fmt.Printf("Timestamp (UTC): %s\n", tid.Time().Format(syntax.AtprotoDatetimeLayout))
	fmt.Printf("Timestamp (Local): %s\n", tid.Time().Local().Format(time.RFC3339))
	fmt.Printf("ClockID: %d\n", tid.ClockID())
	fmt.Printf("uint64: 0x%x\n", tid.Integer())
	return nil
}

func runSyntaxRecordKeyCheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseRecordKey(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

func runSyntaxDIDCheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseDID(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}
func runSyntaxHandleCheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseHandle(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

func runSyntaxNSIDCheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseNSID(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

func runSyntaxATURICheck(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as argument")
	}
	_, err := syntax.ParseATURI(s)
	if err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}
