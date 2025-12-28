package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/PaesslerAG/jsonpath"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/earthboundkid/versioninfo/v2"
)

func userAgent() string {
	return fmt.Sprintf("tap/%s", versioninfo.Short())
}

func backoff(retries int, max int) time.Duration {
	dur := 1 << retries
	if dur > max {
		dur = max
	}

	jitter := time.Millisecond * time.Duration(rand.Intn(1000))
	return time.Second*time.Duration(dur) + jitter
}

// matchesCollection checks if a collection matches any of the provided filters.
// Filters support wildcards at the end (e.g., "app.bsky.*" & "app.bsky.feed.*" both match "app.bsky.feed.post").
// If no filters are provided, all collections match.
func matchesCollection(collection string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}

	for _, filter := range filters {
		if strings.HasSuffix(filter, "*") {
			prefix := strings.TrimSuffix(filter, "*")
			if strings.HasPrefix(collection, prefix) {
				return true
			}
		} else {
			if collection == filter {
				return true
			}
		}
	}

	return false
}

func evtHasSignalCollection(evt *comatproto.SyncSubscribeRepos_Commit, signalColl string) bool {
	for _, op := range evt.Ops {
		collection, _, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			continue
		}
		if collection.String() == signalColl {
			return true
		}
	}
	return false
}

func isRateLimitError(err error) bool {
	var xrpcErr *atclient.APIError
	if errors.As(err, &xrpcErr) {
		return xrpcErr.StatusCode == http.StatusTooManyRequests
	}
	return false
}

func parseOutboxMode(webhookURL string, disableAcks bool) OutboxMode {
	if webhookURL != "" {
		return OutboxModeWebhook
	} else if disableAcks {
		return OutboxModeFireAndForget
	} else {
		return OutboxModeWebsocketAck
	}
}

func matchesRecordContentFilters(logger *slog.Logger, collStr string, rec map[string]any, recordContentFilters []string) bool {
	re := regexp.MustCompile(`^(\S+)\s*:\s*(\S+)\s*=~\s*(r)?"(.*?)"$`)
	// defaults to true
	recordMatches := true

	for _, recordContentFilter := range recordContentFilters {
		// recordContentFilter format: collection: jsonpath =~ (r)?"matchstring"
		matches := re.FindStringSubmatch(recordContentFilter)
		if len(matches) == 0 {
			logger.Warn("invalid recordContentFilter", "filter", recordContentFilter)
			continue
		}
		matchCollStr := strings.TrimSpace(matches[1])
		if collStr != matchCollStr {
			// this filter is for another collection
			continue
		}

		jsonPath := strings.TrimSpace(matches[2])
		rawFlag := len(matches[3]) > 0
		var matchStr string
		if rawFlag {
			matchStr = strings.TrimSpace(matches[4])
		} else {
			matchStr = strings.Replace(
				regexp.QuoteMeta(
					strings.TrimSpace(matches[4])), `\`, `\\`, -1)
		}

		finalExpr := fmt.Sprintf(`%s =~ "%s"`, jsonPath, matchStr)
		matchResult, err := gval.Full(jsonpath.PlaceholderExtension()).Evaluate(finalExpr, rec)
		if err != nil {
			logger.Warn("failed to compile recordContentFilter", "filter", recordContentFilter, "error", err)
			continue
		}

		if matchResult == true {
			recordMatches = true
			break
		} else {
			// this one doesn't match, but perhaps another one does
			recordMatches = false
		}
	}
	return recordMatches
}
