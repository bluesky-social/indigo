/*
Write-to-tarfiles storage interface
*/

package store

import (
	"fmt"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

type TarfilesStore struct {
	// The directory to store the .tar files
	// Each repository is stored as a single .tar file
	// The contents of the .tar file is a collection of json files
	// The directory structure is based on the cllections
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
