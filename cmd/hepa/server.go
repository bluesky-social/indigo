package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/rules"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	bgshost string
	logger  *slog.Logger
	engine  *automod.Engine
}

type Config struct {
	BGSHost string
	Logger  *slog.Logger
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

	engine := automod.Engine{
		Logger:      logger,
		Directory:   dir,
		Counters:    automod.NewMemCountStore(),
		Sets:        automod.NewMemSetStore(),
		Rules:       rules.DefaultRules(),
		AdminClient: nil, // TODO: AppView with mod access, via config
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
