package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	_ "net/http/pprof"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Logger         *slog.Logger
	FDBClusterFile string
}

type server struct {
	cfg Config
	log *slog.Logger

	httpServer    *http.Server
	metricsServer *http.Server
}

func New(config Config) (*server, error) {
	s := &server{
		cfg: config,
		log: config.Logger,
	}

	return s, nil
}

func (s *server) Start(addr string) error {
	handler := s.recoverMiddleware(s.router())

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ErrorLog:     slog.NewLogLogger(s.log.Handler(), slog.LevelError),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

func (s *server) router() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /ping", s.handleHealth)
	mux.HandleFunc("GET /_health", s.handleHealth)
	mux.HandleFunc("GET /xrpc/_health", s.handleHealth)

	return mux
}

func (s *server) RunMetrics(addr string) error {
	mux := http.NewServeMux()

	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)

	s.metricsServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ErrorLog:     slog.NewLogLogger(s.log.Handler(), slog.LevelError),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s.metricsServer.ListenAndServe()
}

func (s *server) Shutdown(ctx context.Context) error {
	var shutdownErr error

	// Shutdown API server
	if s.httpServer != nil {
		s.httpServer.SetKeepAlivesEnabled(false)
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.log.Error("error shutting down API server", "error", err)
			shutdownErr = err
		}
	}

	// Shutdown metrics server
	if s.metricsServer != nil {
		s.metricsServer.SetKeepAlivesEnabled(false)
		if err := s.metricsServer.Shutdown(ctx); err != nil {
			s.log.Error("error shutting down metrics server", "error", err)
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	}

	return shutdownErr
}

func (s *server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.log.Error("panic recovered", "error", err, "path", r.URL.Path)
				s.internalError(w, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.jsonOK(w, map[string]string{
		"status": "ok",
	})
}

func (s *server) plaintextOK(w http.ResponseWriter, msg string, args ...any) {
	s.plaintextWithCode(w, http.StatusOK, msg, args...)
}

func (s *server) plaintextWithCode(w http.ResponseWriter, code int, msg string, args ...any) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, msg, args...) // nolint:errcheck
}

func (s *server) jsonOK(w http.ResponseWriter, resp any) {
	s.jsonWithCode(w, http.StatusOK, resp)
}

func (s *server) jsonWithCode(w http.ResponseWriter, code int, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("failed to encode json response", "error", err)
	}
}

func (s *server) internalError(w http.ResponseWriter, message string) {
	s.jsonWithCode(w, http.StatusInternalServerError, map[string]string{
		"error":   "InternalServerError",
		"message": message,
	})
}
