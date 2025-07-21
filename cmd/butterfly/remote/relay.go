/*
Relay remote interface
*/
package remote

import "fmt"

type RelayRemote struct {
	service string
}

func (self RelayRemote) ListRepos(params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self RelayRemote) FetchRepo(params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self RelayRemote) SubscribeRecords(params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
