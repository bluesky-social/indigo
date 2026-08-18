package cliutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/xrpc"

	slogGorm "github.com/orandin/slog-gorm"
	"github.com/urfave/cli/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
