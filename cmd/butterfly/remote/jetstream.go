/*
Jetstream remote interface
*/
package remote

import (
	"context"
	"fmt"
)

type JetstreamRemote struct {
	Service string
}

// ListRepos not supported by jetstream
func (self JetstreamRemote) ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("list repos: %w", ErrNotSupported)
}

// FetchRepo not supported by jetstream
func (self JetstreamRemote) FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("fetch repo: %w", ErrNotSupported)
}

func (self JetstreamRemote) SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
