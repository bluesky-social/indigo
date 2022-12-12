package main

import (
	"github.com/urfave/cli/v2"
	server "github.com/whyrusleeping/gosky/api/server"
	"github.com/whyrusleeping/gosky/carstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	app := cli.NewApp()

	app.Action = func(cctx *cli.Context) error {
		db, err := gorm.Open(sqlite.Open("pdsdata/pds.db"))
		if err != nil {
			return err
		}

		carstdb, err := gorm.Open(sqlite.Open("pdsdata/carstore.db"))
		if err != nil {
			return err
		}

		cs, err := carstore.NewCarStore(carstdb, "pdsdata/carstore")
		if err != nil {
			return err
		}

		srv, err := server.NewServer(db, cs, "server.key")
		if err != nil {
			return err
		}

		return srv.RunAPI(":4989")
	}

	app.RunAndExitOnError()
}
