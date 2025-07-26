/*
Jetstream remote interface
*/
package remote

import (
	"fmt"
)

type JetstreamRemote struct {
	service string
}

// ListRepos not supported by jetstream
func (self JetstreamRemote) ListRepos(params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("list repos: %w", ErrNotSupported)
}

// FetchRepo not supported by jetstream
func (self JetstreamRemote) FetchRepo(params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("fetch repo: %w", ErrNotSupported)
}

func (self JetstreamRemote) SubscribeRecords(params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
