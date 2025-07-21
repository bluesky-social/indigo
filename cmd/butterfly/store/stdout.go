/*
Dump-to-stdout storage interface
*/

package store

import (
	"fmt"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

const (
	StdoutStoreModePassthrough = iota
	StdoutStoreModeStats
)

type StdoutStore struct {
	Mode int

	// stats
	// TODO: should support multiple repos
	Did        string
	NumRecords uint
}

func (self *StdoutStore) Setup() error {
	return nil
}

func (self *StdoutStore) Close() error {
	return nil
}

func (self *StdoutStore) Receive(s *remote.RemoteStream) error {
	for event := range s.Ch {
		if self.Did == "" {
			self.Did = event.Did
		}

		switch self.Mode {
		case StdoutStoreModePassthrough:
			fmt.Println(event)
		case StdoutStoreModeStats:
			if event.Kind == "commit" && event.Commit.Operation == "create" {
				self.NumRecords++
			}
		}
	}

	if self.Mode == StdoutStoreModeStats {
		// TODO make this more interesting
		fmt.Printf("Stats for repo %s\n", self.Did)
		fmt.Printf("%d records", self.NumRecords)
	}

	return nil
}
