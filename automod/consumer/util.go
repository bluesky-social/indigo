package consumer

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// TODO: move this to a "ParsePath" helper in syntax package?
func splitRepoPath(path string) (syntax.NSID, syntax.RecordKey, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid record path: %s", path)
	}
	collection, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return "", "", err
	}
	rkey, err := syntax.ParseRecordKey(parts[1])
	if err != nil {
		return "", "", err
	}
	return collection, rkey, nil
}
