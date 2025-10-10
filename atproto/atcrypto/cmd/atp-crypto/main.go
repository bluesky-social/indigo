package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/atcrypto"

	"github.com/urfave/cli/v3"
)

func main() {
	app := cli.Command{
		Name:  "atp-crypto",
		Usage: "informal debugging CLI tool for atproto key and cryptography",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:  "generate",
			Usage: "create a new private key",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "p256",
					Usage: "generate a P-256 / secp256r1 / ES256 private key (default)",
				},
				&cli.BoolFlag{
					Name:  "k256",
					Usage: "generate a K-256 / secp256k1 / ES256K private key",
				},
			},

			Action: runGenerate,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(-1)
	}
}

func runGenerate(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("k256") {
		priv, err := atcrypto.GeneratePrivateKeyK256()
		if err != nil {
			return err
		}
		fmt.Println(priv.Multibase())
	} else {
		priv, err := atcrypto.GeneratePrivateKeyP256()
		if err != nil {
			return err
		}
		fmt.Println(priv.Multibase())
	}
	return nil
}
