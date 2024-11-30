package cliutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/xrpc"
	slogGorm "github.com/orandin/slog-gorm"
	"github.com/urfave/cli/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func GetDidResolver(cctx *cli.Context) did.Resolver {
	mr := did.NewMultiResolver()
	mr.AddHandler("plc", &api.PLCServer{
		Host: cctx.String("plc"),
	})
	mr.AddHandler("web", &did.WebResolver{})

	return mr
}

func GetPLCClient(cctx *cli.Context) *api.PLCServer {
	return &api.PLCServer{
		Host: cctx.String("plc"),
	}
}

func NewHttpClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

type CliConfig struct {
	filename string
	PDS      string
}

func readGoskyConfig() (*CliConfig, error) {
	// TODO: use os.UserConfigDir()/gosky, falling back to os.UserHomeDir()/.gosky for backwards compatibility.
	d, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot read Home directory")
	}

	f := filepath.Join(d, ".gosky")

	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, nil
	}

	var out CliConfig
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	out.filename = f
	return &out, nil
}

var Config *CliConfig

func TryReadConfig() {
	cfg, err := readGoskyConfig()
	if err != nil {
		fmt.Println(err)
	} else {
		Config = cfg
	}
}

func WriteConfig(cfg *CliConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(cfg.filename, b, 0664)
}

func GetXrpcClient(cctx *cli.Context, authreq bool) (*xrpc.Client, error) {
	h := "http://localhost:4989"
	if pdsurl := cctx.String("pds-host"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cctx, authreq)
	if err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	return &xrpc.Client{
		Client: NewHttpClient(),
		Host:   h,
		Auth:   auth,
	}, nil
}

func loadAuthFromEnv(cctx *cli.Context, req bool) (*xrpc.AuthInfo, error) {
	if a := cctx.String("auth"); a != "" {
		if ai, err := ReadAuth(a); err != nil && req {
			return nil, err
		} else {
			return ai, nil
		}
	}

	val := os.Getenv("ATP_AUTH_FILE")
	if val == "" {
		if req {
			return nil, fmt.Errorf("no auth env present, ATP_AUTH_FILE not set")
		}

		return nil, nil
	}

	var auth xrpc.AuthInfo
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

func ReadAuth(fname string) (*xrpc.AuthInfo, error) {
	b, err := os.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	var auth xrpc.AuthInfo
	if err := json.Unmarshal(b, &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

// Supports both previous "dbtype=" prefixed DSNs, and URI-style database config strings, for both sqlite and postgresql.
//
// Examples:
// - "sqlite=dir/file.sqlite"
// - "sqlite://file.sqlite"
// - "postgres=host=localhost user=postgres password=password dbname=pdsdb port=5432 sslmode=disable"
// - "postgresql://postgres:password@localhost:5432/pdsdb?sslmode=disable"
func SetupDatabase(dburl string, maxConnections int) (*gorm.DB, error) {
	var dial gorm.Dialector
	// NOTE(bnewbold): might also handle file:// as sqlite, but let's keep it
	// explicit for now

	isSqlite := false
	openConns := maxConnections
	if strings.HasPrefix(dburl, "sqlite://") {
		sqliteSuffix := dburl[len("sqlite://"):]
		// if this isn't ":memory:", ensure that directory exists (eg, if db
		// file is being initialized)
		if !strings.Contains(sqliteSuffix, ":?") {
			os.MkdirAll(filepath.Dir(sqliteSuffix), os.ModePerm)
		}
		dial = sqlite.Open(sqliteSuffix)
		openConns = 1
		isSqlite = true
	} else if strings.HasPrefix(dburl, "sqlite=") {
		sqliteSuffix := dburl[len("sqlite="):]
		// if this isn't ":memory:", ensure that directory exists (eg, if db
		// file is being initialized)
		if !strings.Contains(sqliteSuffix, ":?") {
			os.MkdirAll(filepath.Dir(sqliteSuffix), os.ModePerm)
		}
		dial = sqlite.Open(sqliteSuffix)
		openConns = 1
		isSqlite = true
	} else if strings.HasPrefix(dburl, "postgresql://") || strings.HasPrefix(dburl, "postgres://") {
		// can pass entire URL, with prefix, to gorm driver
		dial = postgres.Open(dburl)
	} else if strings.HasPrefix(dburl, "postgres=") {
		dsn := dburl[len("postgres="):]
		dial = postgres.Open(dsn)
	} else {
		// TODO(bnewbold): this might print password?
		return nil, fmt.Errorf("unsupported or unrecognized DATABASE_URL value: %s", dburl)
	}

	gormLogger := slogGorm.New()

	db, err := gorm.Open(dial, &gorm.Config{
		SkipDefaultTransaction: true,
		TranslateError:         true,
		Logger:                 gormLogger,
	})
	if err != nil {
		return nil, err
	}

	sqldb, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqldb.SetMaxIdleConns(80)
	sqldb.SetMaxOpenConns(openConns)
	sqldb.SetConnMaxIdleTime(time.Hour)

	if isSqlite {
		// Set pragmas for sqlite
		if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
			return nil, err
		}
		if err := db.Exec("PRAGMA synchronous=normal;").Error; err != nil {
			return nil, err
		}
	}

	return db, nil
}

type LogOptions struct {
	LogRotateBytes int64
	LogPath        string
	LogFormat      string
	LogLevel       string
}

// SetupSlog integrates passed in options and env vars.
//
// passing default cliutil.LogOptions{} is ok.
//
// GOLOG_LOG_LEVEL=info|debug|warn|error
//
// GOLOG_LOG_FMT=text|json
//
// GOLOG_FILE=path (or "-" or "" for stdout), %T gets UnixMilli; if a path with '/', {prefix}/current becomes a link to active log file
//
// GOLOG_ROTATE_BYTES=int maximum size of log chunk before rotating
//
// (env vars derived from ipfs logging library)
func SetupSlog(options LogOptions) (*slog.Logger, error) {
	var hopts slog.HandlerOptions
	hopts.Level = slog.LevelInfo
	hopts.AddSource = true
	if options.LogLevel == "" {
		options.LogLevel = os.Getenv("GOLOG_LOG_LEVEL")
	}
	if options.LogLevel == "" {
		hopts.Level = slog.LevelInfo
		options.LogLevel = "info"
	} else {
		level := strings.ToLower(options.LogLevel)
		switch level {
		case "debug":
			hopts.Level = slog.LevelDebug
		case "info":
			hopts.Level = slog.LevelInfo
		case "warn":
			hopts.Level = slog.LevelWarn
		case "error":
			hopts.Level = slog.LevelError
		default:
			return nil, fmt.Errorf("unknown log level: %#v", options.LogLevel)
		}
	}
	if options.LogFormat == "" {
		options.LogFormat = os.Getenv("GOLOG_LOG_FMT")
	}
	if options.LogFormat == "" {
		options.LogFormat = "text"
	} else {
		format := strings.ToLower(options.LogFormat)
		if format == "json" || format == "text" {
			// ok
		} else {
			return nil, fmt.Errorf("invalid log format: %#v", options.LogFormat)
		}
		options.LogFormat = format
	}

	if options.LogPath == "" {
		options.LogPath = os.Getenv("GOLOG_FILE")
	}
	if options.LogRotateBytes == 0 {
		rotateBytesStr := os.Getenv("GOLOG_ROTATE_BYTES")
		if rotateBytesStr != "" {
			rotateBytes, err := strconv.ParseInt(rotateBytesStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid GOLOG_ROTATE_BYTES value: %w", err)
			}
			options.LogRotateBytes = rotateBytes
		}
	}
	var out io.Writer
	if (options.LogPath == "") || (options.LogPath == "-") {
		out = os.Stdout
	} else if options.LogRotateBytes != 0 {
		out = &logRotateWriter{
			rotateBytes:     options.LogRotateBytes,
			outPathTemplate: options.LogPath,
		}
	} else {
		var err error
		out, err = os.Create(options.LogPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", options.LogPath, err)
		}
	}
	var handler slog.Handler
	switch options.LogFormat {
	case "text":
		handler = slog.NewTextHandler(out, &hopts)
	case "json":
		handler = slog.NewJSONHandler(out, &hopts)
	default:
		return nil, fmt.Errorf("unknown log format: %#v", options.LogFormat)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}

type logRotateWriter struct {
	currentWriter io.WriteCloser

	// how much has been written to current log file
	currentBytes int64

	// e.g. path/to/logs/foo%T
	currentPath string

	// e.g. path/to/logs/current
	currentPathCurrent string

	rotateBytes int64

	outPathTemplate string
}

func (w *logRotateWriter) Write(p []byte) (n int, err error) {
	var earlyWeakErrors []error
	if int64(len(p))+w.currentBytes > w.rotateBytes {
		// next write would be over the limit
		if w.currentWriter != nil {
			err := w.currentWriter.Close()
			if err != nil {
				earlyWeakErrors = append(earlyWeakErrors, err)
			}
			w.currentWriter = nil
			w.currentBytes = 0
			w.currentPath = ""
			if w.currentPathCurrent != "" {
				err = os.Remove(w.currentPathCurrent) // not really an error until something else goes wrong
				if err != nil {
					earlyWeakErrors = append(earlyWeakErrors, err)
				}
				w.currentPathCurrent = ""
			}
		}
	}
	if w.currentWriter == nil {
		// start new log file
		nowMillis := time.Now().UnixMilli()
		nows := strconv.FormatInt(nowMillis, 10)
		w.currentPath = strings.Replace(w.outPathTemplate, "%T", nows, -1)
		var err error
		w.currentWriter, err = os.Create(w.currentPath)
		if err != nil {
			earlyWeakErrors = append(earlyWeakErrors, err)
			return 0, errors.Join(earlyWeakErrors...)
		}
		dirpart, _ := filepath.Split(w.currentPath)
		if dirpart != "" {
			w.currentPathCurrent = filepath.Join(dirpart, "current")
			fi, err := os.Stat(w.currentPathCurrent)
			if err == nil && fi.Mode().IsRegular() {
				// move aside unknown "current" from a previous run
				err = os.Rename(w.currentPathCurrent, w.currentPathCurrent+"_"+nows)
				if err != nil {
					// not crucial if we can't move aside "current"
					// TODO: log warning ... but not from inside log writer?
					earlyWeakErrors = append(earlyWeakErrors, err)
				}
			}
			err = os.Link(w.currentPath, w.currentPathCurrent)
			if err != nil {
				// not crucial if we can't make "current" link
				// TODO: log warning ... but not from inside log writer?
				earlyWeakErrors = append(earlyWeakErrors, err)
			}
		}
	}
	var wrote int
	wrote, err = w.currentWriter.Write(p)
	w.currentBytes += int64(wrote)
	if err != nil {
		earlyWeakErrors = append(earlyWeakErrors, err)
		return wrote, errors.Join(earlyWeakErrors...)
	}
	return wrote, nil
}
