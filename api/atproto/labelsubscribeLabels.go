package atproto

// schema: com.atproto.label.subscribeLabels

func init() {
}

type LabelSubscribeLabels_Info struct {
	Message *string `json:"message,omitempty" cborgen:"message,omitempty"`
	Name    string  `json:"name" cborgen:"name"`
}

type LabelSubscribeLabels_Labels struct {
	Labels []*LabelDefs_Label `json:"labels" cborgen:"labels"`
	Seq    int64              `json:"seq" cborgen:"seq"`
}
