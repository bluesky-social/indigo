package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/rules"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	bgshost string
	logger  *slog.Logger
	engine  *automod.Engine
}

type Config struct {
	BGSHost       string
	ModHost       string
	ModAdminToken string
	ModUsername   string
	ModPassword   string
	SetsFileJSON  string
	Logger        *slog.Logger
}

func NewServer(dir identity.Directory, config Config) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	bgsws := config.BGSHost
	if !strings.HasPrefix(bgsws, "ws") {
		return nil, fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
	}

	// TODO: this isn't a very robust way to handle a peristent client
	var xrpcc *xrpc.Client
	if config.ModAdminToken != "" {
		xrpcc = &xrpc.Client{
			Client:     util.RobustHTTPClient(),
			Host:       config.ModHost,
			AdminToken: &config.ModAdminToken,
			Auth:       &xrpc.AuthInfo{},
		}

		auth, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
			Identifier: config.ModUsername,
			Password:   config.ModPassword,
		})
		if err != nil {
			return nil, err
		}
		xrpcc.Auth.AccessJwt = auth.AccessJwt
		xrpcc.Auth.RefreshJwt = auth.RefreshJwt
		xrpcc.Auth.Did = auth.Did
		xrpcc.Auth.Handle = auth.Handle
	}

	sets := automod.NewMemSetStore()
	if config.SetsFileJSON != "" {
		if err := sets.LoadFromFileJSON(config.SetsFileJSON); err != nil {
			return nil, err
		} else {
			logger.Info("loaded set config from JSON", "path", config.SetsFileJSON)
		}
	}

	engine := automod.Engine{
		Logger:      logger,
		Directory:   dir,
		Counters:    automod.NewMemCountStore(),
		Sets:        sets,
		Rules:       rules.DefaultRules(),
		AdminClient: xrpcc,
	}

	s := &Server{
		bgshost: config.BGSHost,
		logger:  logger,
		engine:  &engine,
	}

	return s, nil
}

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}
