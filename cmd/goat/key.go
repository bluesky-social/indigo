package main

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/crypto"

	"github.com/urfave/cli/v2"
)

var cmdKey = &cli.Command{
	Name:  "key",
	Usage: "sub-commands for cryptographic keys",
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:  "generate",
			Usage: "outputs a new secret key",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "type",
					Aliases: []string{"t"},
					Usage:   "indicate curve type (P-256 is default)",
				},
				&cli.BoolFlag{
					Name:  "terse",
					Usage: "print just the secret key, in multikey format",
				},
			},
			Action: runKeyGenerate,
		},
		&cli.Command{
			Name:      "inspect",
			Usage:     "parses and outputs metadata about a public or secret key",
			ArgsUsage: `<key>`,
			Action:    runKeyInspect,
		},
	},
}

func runKeyGenerate(cctx *cli.Context) error {
	var priv crypto.PrivateKey
	var privMultibase string
	switch cctx.String("type") {
	case "", "P-256", "p256", "ES256", "secp256r1":
		sec, err := crypto.GeneratePrivateKeyP256()
		if err != nil {
			return err
		}
		privMultibase = sec.Multibase()
		priv = sec
	case "K-256", "k256", "ES256K", "secp256k1":
		sec, err := crypto.GeneratePrivateKeyK256()
		if err != nil {
			return err
		}
		privMultibase = sec.Multibase()
		priv = sec
	default:
		return fmt.Errorf("unknown key type: %s", cctx.String("type"))
	}
	if cctx.Bool("terse") {
		fmt.Println(privMultibase)
		return nil
	}
	pub, err := priv.PublicKey()
	if err != nil {
		return err
	}
	fmt.Printf("Key Type: %s\n", descKeyType(priv))
	fmt.Printf("Secret Key (Multibase Syntax): save this securely (eg, add to password manager)\n\t%s\n", privMultibase)
	fmt.Printf("Public Key (DID Key Syntax): share or publish this (eg, in DID document)\n\t%s\n", pub.DIDKey())
	return nil
}

func descKeyType(val interface{}) string {
	switch val.(type) {
	case *crypto.PublicKeyP256, crypto.PublicKeyP256:
		return "P-256 / secp256r1 / ES256 public key"
	case *crypto.PrivateKeyP256, crypto.PrivateKeyP256:
		return "P-256 / secp256r1 / ES256 private key"
	case *crypto.PublicKeyK256, crypto.PublicKeyK256:
		return "K-256 / secp256k1 / ES256K public key"
	case *crypto.PrivateKeyK256, crypto.PrivateKeyK256:
		return "K-256 / secp256k1 / ES256K private key"
	default:
		return "unknown"
	}
}

func runKeyInspect(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide key as an argument")
	}

	sec, err := crypto.ParsePrivateMultibase(s)
	if nil == err {
		fmt.Printf("Type: %s\n", descKeyType(sec))
		fmt.Printf("Encoding: multibase\n")
		pub, err := sec.PublicKey()
		if err != nil {
			return err
		}
		fmt.Printf("Public (DID Key): %s\n", pub.DIDKey())
		return nil
	}

	pub, err := crypto.ParsePublicMultibase(s)
	if nil == err {
		fmt.Printf("Type: %s\n", descKeyType(pub))
		fmt.Printf("Encoding: multibase\n")
		fmt.Printf("As DID Key: %s\n", pub.DIDKey())
		return nil
	}

	pub, err = crypto.ParsePublicDIDKey(s)
	if nil == err {
		fmt.Printf("Type: %s\n", descKeyType(pub))
		fmt.Printf("Encoding: DID Key\n")
		fmt.Printf("As Multibase: %s\n", pub.Multibase())
		return nil
	}
	return fmt.Errorf("unknown key encoding or type")
}
