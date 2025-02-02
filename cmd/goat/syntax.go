package main

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v3"
)

var cmdSyntax = &cli.Command{
	Name:  "syntax",
	Usage: "sub-commands for string syntax helpers",
	Commands: []*cli.Command{
		&cli.Command{
			Name:  "tid",
			Usage: "sub-commands for TIDs",
			Commands: []*cli.Command{
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
			Commands: []*cli.Command{
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
			Commands: []*cli.Command{
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
			Commands: []*cli.Command{
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
			Commands: []*cli.Command{
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
			Commands: []*cli.Command{
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

func runSyntaxTIDCheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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

func runSyntaxTIDGenerate(ctx context.Context, cmd *cli.Command) error {
	fmt.Printf("%s\n", syntax.NewTIDNow(0).String())
	return nil
}

func runSyntaxTIDInspect(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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

func runSyntaxRecordKeyCheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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

func runSyntaxDIDCheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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
func runSyntaxHandleCheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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

func runSyntaxNSIDCheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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

func runSyntaxATURICheck(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
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
