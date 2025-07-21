/*
Jetstream remote interface
*/
package remote

import "fmt"

type JetstreamRemote struct {
	service string
}

func (self JetstreamRemote) ListRepos(params ListReposParams) (*ListReposResult, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self JetstreamRemote) FetchRepo(params FetchRepoParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}

func (self JetstreamRemote) SubscribeRecords(params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("Not yet implemented")
}
