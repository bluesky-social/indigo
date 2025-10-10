package main

type Commit struct {
	Did     string     `json:"did"`
	Rev     string     `json:"rev"`
	DataCid string     `json:"data_cid"`
	Ops     []CommitOp `json:"ops"`
}

type CommitOp struct {
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

func (c *Commit) ToEvts() []*RecordEvt {
	var evts []*RecordEvt
	for _, op := range c.Ops {
		evts = append(evts, &RecordEvt{
			Did:        c.Did,
			Rev:        c.Rev,
			Collection: op.Collection,
			Rkey:       op.Rkey,
			Action:     op.Action,
			Record:     op.Record,
			Cid:        op.Cid,
		})
	}
	return evts
}

type RecordEvt struct {
	Did        string                 `json:"did"`
	Rev        string                 `json:"rev"`
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

type UserEvt struct {
	Did      string `json:"did"`
	Handle   string `json:"handle"`
	IsActive bool   `json:"is_active"`
	Status   string `json:"status"`
}

type OutboxEvt struct {
	Type      string     `json:"type"`
	RecordEvt *RecordEvt `json:"evt,omitempty"`
	UserEvt   *UserEvt   `json:"evt,omitempty"`
}
