/*
PDS remote interface
*/
package remote

import "fmt"

type PdsRemote struct {
	service string
}

func (self PdsRemote) ListRepos(params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self PdsRemote) FetchRepo(params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self PdsRemote) SubscribeRecords(params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
