package main

import (
	"fmt"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/bluesky-social/indigo/cmd/butterfly/store"
)

func main() {
	r := remote.CarfileRemote{Filepath: "/Users/paulfrazee/tmp/carfiles/pfrazee.car"}
	s := store.StdoutStore{Mode: store.StdoutStoreModeStats}

	if err := s.Setup(); err != nil {
		fmt.Println(err)
		return
	}
	defer s.Close()

	res, err := r.FetchRepo(remote.FetchRepoParams{Did: "did:plc:ragtjsm2j2vknwkz3zp4oxrd"})
	if err != nil {
		fmt.Println(err)
		return
	}

	s.Receive(res)
}
