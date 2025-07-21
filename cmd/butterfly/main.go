package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	fmt.Println(WhereClause{})
	fmt.Println(WhereClause{Repo: "pfrazee.com", Collection: "app.bsky.graph.follow", Attr: "subject"})

	docJson := `{"select": [{"where": {"repo": "pfrazee.com"},"tag": "user"}],"retain": {"user": {"*": "*"}}}`
	var doc SelectorDoc
	json.Unmarshal([]byte(docJson), &doc)
	fmt.Println((doc))
}
