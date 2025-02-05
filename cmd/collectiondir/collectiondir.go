package main

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func main() {
	app := cli.App{
		Name:    "collectiondir",
		Usage:   "collection directory service",
		Version: versioninfo.Short(),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "verbose",
			},
		},
		Commands: []*cli.Command{
			serveCmd,
			crawlCmd,
			buildCmd,
			statsCmd,
			exportCmd,
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

var statsCmd = &cli.Command{
	Name:  "stats",
	Usage: "read stats from a pebble db",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "pebble",
			Usage:    "path to pebble db",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		logLevel := slog.LevelInfo
		if cctx.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cctx.String("pebble")
		var db PebbleCollectionDirectory
		db.log = log
		err := db.Open(pebblePath)
		if err != nil {
			return err
		}
		defer db.Close()

		stats, err := db.GetCollectionStats()
		if err != nil {
			return err
		}
		blob, err := json.MarshalIndent(stats, "", "  ")
		os.Stdout.Write(blob)
		os.Stdout.Write([]byte{'\n'})
		return nil
	},
}

var exportCmd = &cli.Command{
	Name:  "export",
	Usage: "export a pebble db to CSV on stdout",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "pebble",
			Usage:    "path to pebble db",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		logLevel := slog.LevelInfo
		if cctx.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cctx.String("pebble")
		var db PebbleCollectionDirectory
		db.log = log
		err := db.Open(pebblePath)
		if err != nil {
			return err
		}
		defer db.Close()

		rows := make(chan CollectionDidTime, 100)
		go func() {
			err := db.ReadAllPrimary(cctx.Context, rows)
			if err != nil {
				log.Error("db read", "path", pebblePath, "err", err)
			}
		}()

		writer := csv.NewWriter(os.Stdout)
		defer writer.Flush()
		err = writer.Write([]string{"did", "collection", "millis"})
		if err != nil {
			log.Error("csv write header", "err", err)
		}
		var row [3]string
		for rowi := range rows {
			row[0] = rowi.Did
			row[1] = rowi.Collection
			row[2] = strconv.FormatInt(rowi.UnixMillis, 10)
			err = writer.Write(row[:])
			if err != nil {
				log.Error("csv write row", "err", err)
			}
		}

		return nil
	},
}

var buildCmd = &cli.Command{
	Name:  "build",
	Usage: "collect csv into a database",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "csv",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "pebble",
			Usage:    "path to store pebble db",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		logLevel := slog.LevelInfo
		if cctx.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cctx.String("pebble")
		var db PebbleCollectionDirectory
		db.log = log
		err := db.Open(pebblePath)
		if err != nil {
			return err
		}
		defer db.Close()
		csvPath := cctx.String("csv")
		var fin io.Reader
		if csvPath == "-" {
			fin = os.Stdin
		} else if strings.HasSuffix(csvPath, ".gz") {
			osin, err := os.Open(csvPath)
			if err != nil {
				return fmt.Errorf("%s: could not open csv, %w", csvPath, err)
			}
			defer osin.Close()
			gzin, err := gzip.NewReader(osin)
			if err != nil {
				return fmt.Errorf("%s: could not open csv, %w", csvPath, err)
			}
			defer gzin.Close()
			fin = gzin
		} else {
			osin, err := os.Open(csvPath)
			if err != nil {
				return fmt.Errorf("%s: could not open csv, %w", csvPath, err)
			}
			defer osin.Close()
			fin = osin
		}
		reader := csv.NewReader(fin)
		rowcount := 0
		results := make(chan DidCollection, 100)
		go db.SetFromResults(results)
		for {
			row, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			did := row[0]
			collection := row[1]
			results <- DidCollection{
				Did:        did,
				Collection: collection,
			}
			rowcount++
		}
		close(results)
		log.Debug("read csv", "rows", rowcount)
		return nil
	},
}
