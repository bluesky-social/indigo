package events

type DiskPersistence struct {
	dir string
}

func NewDiskPersistence(dir string) (*DiskPersistence, error) {
	return &DiskPersistence{
		dir: dir,
	}, nil
}

func (p *DiskPersistence) Persist(e *RepoStreamEvent) error {
	panic("nyi")
}

func (p *DiskPersistence) Playback(since int64, cb func(*RepoStreamEvent) error) error {
	panic("nyi")
}
