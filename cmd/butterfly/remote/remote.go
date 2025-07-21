package remote

type Remote interface {
	// Lists repositories hosted at the given remote. Not all parameters will be supported.
	ListRepos(params ListReposParams) (ListReposResult, error)

	// Fetches the contents of the requested repositories. Not all parameters will be supported.
	FetchRepo(params FetchRepoParams) (RemoteStream, error)

	// Subscribes to the record event-stream of the remote. Not all parameters will be supported.
	SubscribeRecords(params SubscribeRecordsParams) (RemoteStream, error)
}

type ListReposParams struct {
	Collection string
}
type ListReposResult struct {
	Dids []string
}

type FetchRepoParams struct {
	Did         string
	Collections []string
}

type SubscribeRecordsParams struct {
	Dids        []string
	Collections []string
}

type RemoteStream struct {
	Ch chan StreamEvent
}

type StreamEvent struct {
	Did      string
	Time     uint
	Kind     string
	Commit   StreamEventCommit
	Identity StreamEventIdentity
	Account  StreamEventAccount
	Error    StreamEventError
}

type StreamEventCommit struct {
	Rev        string
	Operation  string
	Collection string
	Rkey       string
	Record     map[string]any
	Cid        string
}
type StreamEventIdentity struct {
	Did    string
	Handle string
	Seq    uint
	Time   string
}
type StreamEventAccount struct {
	Active bool
	Did    string
	Seq    uint
	Time   string
}
type StreamEventError struct {
	Err error
}
