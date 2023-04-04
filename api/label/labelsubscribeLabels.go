package label

// schema: com.atproto.label.subscribeLabels

func init() {
}

type SubscribeLabels_Info struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Message       *string `json:"message,omitempty" cborgen:"message,omitempty"`
	Name          string  `json:"name" cborgen:"name"`
}

type SubscribeLabels_Labels struct {
	LexiconTypeID string   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Labels        []*Label `json:"labels" cborgen:"labels"`
	Seq           int64    `json:"seq" cborgen:"seq"`
}
