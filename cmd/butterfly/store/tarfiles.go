/*
Write-to-tarfiles storage interface
*/

package store

import (
	"fmt"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

type TarfilesStore struct {
	dirpath string
}

func (self *TarfilesStore) Setup() error {
	return fmt.Errorf("Not yet implemented")
}

func (self *TarfilesStore) Close() error {
	return fmt.Errorf("Not yet implemented")
}

func (self *TarfilesStore) Receive(s *remote.RemoteStream) error {
	return fmt.Errorf("Not yet implemented")
}
