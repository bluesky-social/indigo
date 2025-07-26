/*
Relay remote interface
*/
package remote

import (
	"context"
	"fmt"
)

type RelayRemote struct {
	Service string
}

func (self RelayRemote) ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self RelayRemote) FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self RelayRemote) SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
