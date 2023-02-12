package api

type PostEntity struct {
	Index *TextSlice `json:"index" cborgen:"index"`
	Type  string     `json:"type" cborgen:"type"`
	Value string     `json:"value" cborgen:"value"`
}

type TextSlice struct {
	Start int64 `json:"start" cborgen:"start"`
	End   int64 `json:"end" cborgen:"end"`
}

type ReplyRef struct {
	Root   PostRef `json:"root" cborgen:"root"`
	Parent PostRef `json:"parent" cborgen:"parent"`
}

type PostRecord struct {
	Type      string        `json:"$type,omitempty" cborgen:"$type"`
	Text      string        `json:"text" cborgen:"text"`
	Entities  []*PostEntity `json:"entities,omitempty" cborgen:"entities"`
	Reply     *ReplyRef     `json:"reply,omitempty" cborgen:"reply"`
	CreatedAt string        `json:"createdAt" cborgen:"createdAt"`
}

type PostRef struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
}

type GetTimelineResp struct {
	Cursor string     `json:"cursor"`
	Feed   []FeedItem `json:"feed"`
}

type FeedItem struct {
	Uri        string      `json:"uri"`
	Cid        string      `json:"cid"`
	Author     *User       `json:"author"`
	RepostedBy *User       `json:"repostedBy"`
	MyState    *MyState    `json:"myState"`
	Record     interface{} `json:"record"`
}

type MyState struct {
	Repost   string `json:"repost"`
	Upvote   string `json:"upvote"`
	Downvote string `json:"downvote"`
}

type Declaration struct {
	Cid       string `json:"cid"`
	ActorType string `json:"actorType"`
}

type User struct {
	Did         string       `json:"did"`
	Handle      string       `json:"handle"`
	DisplayName string       `json:"displayName"`
	Declaration *Declaration `json:"declaration"`
}

type GSADeclaration struct {
	Cid       string `json:"cid"`
	ActorType string `json:"actorType"`
}

type GetSuggestionsActor struct {
	Did         string          `json:"did"`
	Declaration *GSADeclaration `json:"declaration"`
	Handle      string          `json:"handle"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	IndexedAt   string          `json:"indexedAt"`
}

type GetSuggestionsResp struct {
	Cursor string                `json:"cursor"`
	Actors []GetSuggestionsActor `json:"actors"`
}

type GetFollowsResp struct {
	Subject *User  `json:"subject"`
	Cursor  string `json:"cursor"`
	Follows []User `json:"follows"`
}
