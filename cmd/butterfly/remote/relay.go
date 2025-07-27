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

func (r *RelayRemote) ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error) {
	return XrpcListRepos(ctx, r.Service, params)
}

func (r *RelayRemote) FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (r *RelayRemote) SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
