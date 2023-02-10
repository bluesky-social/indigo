package atproto

// schema: com.atproto.report.subject

func init() {
}

type ReportSubject_Record struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Cid           *string `json:"cid,omitempty" cborgen:"cid"`
	Collection    string  `json:"collection" cborgen:"collection"`
	Did           string  `json:"did" cborgen:"did"`
	Rkey          string  `json:"rkey" cborgen:"rkey"`
}

type ReportSubject_RecordRef struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}

type ReportSubject_Repo struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}
