package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	api "github.com/bluesky-social/indigo/api"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/xrpc"
	lru "github.com/hashicorp/golang-lru"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	es "github.com/opensearch-project/opensearch-go/v2"

	cli "github.com/urfave/cli/v2"
)

var log = logging.Logger("search")

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{}
	app.Commands = []*cli.Command{
		elasticCheckCmd,
		searchCmd,
		runCmd,
	}

	app.RunAndExitOnError()
}

var runCmd = &cli.Command{
	Name: "run",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Value:   "sqlite://data/thecloud.db",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.StringFlag{
			Name:     "atp-bgs-host",
			Required: true,
			EnvVars:  []string{"ATP_BGS_HOST"},
		},
		&cli.BoolFlag{
			Name:    "readonly",
			EnvVars: []string{"READONLY"},
		},
		&cli.StringFlag{
			Name: "elastic-cert",
		},
		&cli.StringFlag{
			Name:  "plc-host",
			Value: "https://plc.directory",
		},
		&cli.StringFlag{
			Name:  "pds-host",
			Value: "https://bsky.social",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Connecting to database")
		db, err := cliutil.SetupDatabase(cctx.String("database-url"))
		if err != nil {
			return err
		}

		log.Info("Migrating database")
		db.AutoMigrate(&PostRef{})
		db.AutoMigrate(&User{})
		db.AutoMigrate(&LastSeq{})

		log.Infof("Configuring ES client")
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return fmt.Errorf("failed to get elasticsearch: %w", err)
		}

		log.Infof("Configuring HTTP server")
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			log.Error(err)
		}

		xc := &xrpc.Client{
			Host: cctx.String("pds-host"),
		}

		plc := &api.PLCServer{
			Host: cctx.String("plc-host"),
		}

		bgsws := cctx.String("atp-bgs-host")
		if !strings.HasPrefix(bgsws, "ws") {
			return fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
		}

		bgshttp := strings.Replace(bgsws, "ws", "http", 1)
		bgsxrpc := &xrpc.Client{
			Host: bgshttp,
		}

		ucache, _ := lru.New(100000)
		s := &Server{
			escli:     escli,
			db:        db,
			bgshost:   cctx.String("atp-bgs-host"),
			xrpcc:     xc,
			bgsxrpc:   bgsxrpc,
			plc:       plc,
			userCache: ucache,
		}

		e.Use(middleware.CORS())

		e.GET("/search/posts", s.handleSearchRequestPosts)
		e.GET("/search/profiles", s.handleSearchRequestProfiles)

		go func() {
			panic(e.Start(":3999"))
		}()

		if cctx.Bool("readonly") {
			select {}
		} else {
			ctx := context.TODO()
			if err := s.Run(ctx); err != nil {
				return fmt.Errorf("failed to run: %w", err)
			}
		}

		return nil
	},
}

func getEsCli(certfi string) (*es.Client, error) {
	user := "elastic"
	if u := os.Getenv("ELASTIC_USERNAME"); u != "" {
		user = u
	}

	addrs := []string{
		"https://192.168.1.221:9200",
	}

	if hosts := os.Getenv("ELASTIC_HOSTS"); hosts != "" {
		addrs = strings.Split(hosts, ",")
	}

	pass := os.Getenv("ELASTIC_PASSWORD")

	var cert []byte
	if certfi != "" {
		b, err := os.ReadFile(certfi)
		if err != nil {
			return nil, err
		}

		cert = b
	}

	cfg := es.Config{
		Addresses: addrs,
		Username:  user,
		Password:  pass,

		CACert: cert,
	}
	escli, err := es.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set up client: %w", err)
	}
	info, err := escli.Info()
	if err != nil {
		return nil, fmt.Errorf("cannot get escli info: %w", err)
	}
	defer info.Body.Close()
	fmt.Println(info)

	return escli, nil
}

var elasticCheckCmd = &cli.Command{
	Name: "elastic-check",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "elastic-cert",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return err
		}

		inf, err := escli.Info()
		if err != nil {
			return fmt.Errorf("failed to get info: %w", err)
		}

		fmt.Println(inf)
		return nil

	},
}

var searchCmd = &cli.Command{
	Name: "search",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "elastic-cert",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := getEsCli(cctx.String("elastic-cert"))
		if err != nil {
			return err
		}

		var buf bytes.Buffer
		query := map[string]interface{}{
			"query": map[string]interface{}{
				"match": map[string]interface{}{
					"text": cctx.Args().First(),
				},
			},
		}
		if err := json.NewEncoder(&buf).Encode(query); err != nil {
			log.Fatalf("Error encoding query: %s", err)
		}

		// Perform the search request.
		res, err := escli.Search(
			escli.Search.WithContext(context.Background()),
			escli.Search.WithIndex("posts"),
			escli.Search.WithBody(&buf),
			escli.Search.WithTrackTotalHits(true),
			escli.Search.WithPretty(),
		)
		if err != nil {
			log.Fatalf("Error getting response: %s", err)
		}

		fmt.Println(res)
		return nil

	},
}
