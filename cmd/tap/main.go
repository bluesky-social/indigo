package main

import (
	"context"
	"log/slog"
	"os"

	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/cmd/tap/runtap"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	if err := runtap.Run(context.Background(), os.Args); err != nil {
		slog.Error("exiting process", "error", err)
		os.Exit(-1)
	}
}
