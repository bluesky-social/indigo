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

func (c *Commit) ToOps() []*Op {
	var ops []*Op
	for _, op := range c.Ops {
		ops = append(ops, &Op{
			Did:        c.Did,
			Rev:        c.Rev,
			Collection: op.Collection,
			Rkey:       op.Rkey,
			Action:     op.Action,
			Record:     op.Record,
			Cid:        op.Cid,
		})
	}
	return ops
}

type Op struct {
	Did        string                 `json:"did"`
	Rev        string                 `json:"rev"`
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}
