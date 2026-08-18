package lex

import "testing"

func TestParsePackages(t *testing.T) {
	text := `[{"package": "bsky", "prefix": "app.bsky", "outdir": "api/bsky", "import": "github.com/bluesky-social/indigo/api/bsky"}]`
	parsed, err := ParsePackages([]byte(text))
	if err != nil {
		t.Fatalf("error parsing json: %s", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1, got %d", len(parsed))
	}
	expected := Package{"bsky", "app.bsky", "api/bsky", "github.com/bluesky-social/indigo/api/bsky"}
	if expected != parsed[0] {
		t.Fatalf("expected %#v, got %#v", expected, parsed[0])
	}

}
