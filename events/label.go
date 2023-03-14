package events

// this is here, instead of under 'labeling' package, to avoid an import loop
type Label struct {
	LexiconTypeID string  `json:"$type" cborgen:"$type,const=app.bsky.label.label"`
	SourceDid     string  `json:"src" cborgen:"src"`
	SubjectUri    string  `json:"uri" cborgen:"uri"`
	SubjectCid    *string `json:"cid,omitempty" cborgen:"cid"`
	Value         string  `json:"val" cborgen:"val"`
	Timestamp     string  `json:"ts" cborgen:"ts"` // TODO: actual timestamp?
	LabelUri      *string `json:"labeluri,omitempty" cborgen:"labeluri"`
}
