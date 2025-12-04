package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
)

func main() {
	app := cli.Command{
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
			offlineCrawlCmd,
			buildCmd,
			statsCmd,
			exportCmd,
			adminCrawlCmd,
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
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
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logLevel := slog.LevelInfo
		if cmd.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cmd.String("pebble")
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
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logLevel := slog.LevelInfo
		if cmd.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cmd.String("pebble")
		var db PebbleCollectionDirectory
		db.log = log
		err := db.Open(pebblePath)
		if err != nil {
			return err
		}
		defer db.Close()

		rows := make(chan CollectionDidTime, 100)
		go func() {
			err := db.ReadAllPrimary(ctx, rows)
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
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logLevel := slog.LevelInfo
		if cmd.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)
		pebblePath := cmd.String("pebble")
		var db PebbleCollectionDirectory
		db.log = log
		err := db.Open(pebblePath)
		if err != nil {
			return err
		}
		defer db.Close()
		csvPath := cmd.String("csv")
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

var adminCrawlCmd = &cli.Command{
	Name:  "crawl",
	Usage: "admin service to crawl one or more PDSes",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "csv",
			Usage: "path to load csv from, use column 'host' or 'hostname'",
		},
		&cli.StringFlag{
			Name:  "list",
			Usage: "path to load hostname list from, one per line",
		},
		&cli.StringFlag{
			Name:     "url",
			Usage:    "host:port of collectiondir server",
			Required: true,
			Sources:  cli.EnvVars("COLLECTIONDIR_URL"),
		},
		&cli.StringFlag{
			Name:    "auth",
			Usage:   "Auth token for admin api",
			Sources: cli.EnvVars("ADMIN_AUTH"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logLevel := slog.LevelInfo
		if cmd.Bool("verbose") {
			logLevel = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		slog.SetDefault(log)

		var hostList []string

		serverUrl, err := url.Parse(cmd.String("url"))
		if err != nil {
			var e2 error
			// try to fixup a bare host:port which can confuse url.Parse
			serverUrl, e2 = url.Parse("http://" + cmd.String("url"))
			if e2 != nil {
				return fmt.Errorf("could not parse url, %w", err)
			}
		}
		requestCrawlUrl := serverUrl.JoinPath("/admin/pds/requestCrawl")

		if cmd.IsSet("list") {
			fin, err := os.Open(cmd.String("list"))
			if err != nil {
				return fmt.Errorf("%s: could not open, %w", cmd.String("list"), err)
			}
			defer fin.Close()
			bufin := bufio.NewScanner(fin)
			for bufin.Scan() {
				hostList = append(hostList, bufin.Text())
			}
			err = bufin.Err()
			if err != nil {
				return fmt.Errorf("%s: error reading, %w", cmd.String("list"), err)
			}
		} else if cmd.IsSet("csv") {
			fin, err := os.Open(cmd.String("csv"))
			if err != nil {
				return fmt.Errorf("%s: could not open, %w", cmd.String("csv"), err)
			}
			defer fin.Close()
			data, err := csv.NewReader(fin).ReadAll()
			if err != nil {
				return fmt.Errorf("%s: could not read, %w", cmd.String("csv"), err)
			}
			if len(data) < 2 {
				return fmt.Errorf("%s: empty CSV file", cmd.String("csv"))
			}
			headerRow := data[0]
			hostCol := -1
			for i, v := range headerRow {
				v = strings.ToLower(v)
				if v == "host" || v == "hostname" {
					hostCol = i
					break
				}
			}
			if hostCol < 0 {
				return fmt.Errorf("%s: header missing 'host' or 'hostname'", cmd.String("csv"))
			}
			for _, row := range data[1:] {
				hostList = append(hostList, row[hostCol])
			}
		}

		if len(hostList) == 0 {
			fmt.Println("no hosts")
		}

		client := http.Client{Timeout: 1 * time.Second}
		var headers http.Header = make(http.Header)
		if cmd.IsSet("auth") {
			headers.Add("Authorization", "Bearer "+cmd.String("auth"))
		}
		headers.Add("Content-Type", "application/json")
		var response *http.Response
		postReqeust := CrawlRequest{
			Hosts: hostList,
		}
		reqBlob, err := json.Marshal(postReqeust)
		reqReader := bytes.NewReader(reqBlob)

		for try := 0; try < 3; try++ {
			req, err := http.NewRequest("POST", requestCrawlUrl.String(), reqReader)
			if err != nil {
				return fmt.Errorf("could not create request, %w", err)
			}
			req.Header = headers
			response, err = client.Do(req)
			if err == nil && response.StatusCode == 200 {
				break
			} else {
				log.Info("http err", "err", err, "status", response.StatusCode)
				if try < 2 {
					time.Sleep(time.Duration(try+1) * 2 * time.Second)
				}
			}
		}
		if err != nil {
			return fmt.Errorf("POST %s err %w", requestCrawlUrl.String(), err)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("POST %s err %s", requestCrawlUrl.String(), response.Status)
		}

		return nil
	},
}
