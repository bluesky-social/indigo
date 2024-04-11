package search

import (
	"net/url"

	"github.com/PuerkitoBio/purell"
)

var trackingParams = []string{
	"__s",
	"_ga",
	"campaign_id",
	"ceid",
	"emci",
	"emdi",
	"fbclid",
	"gclid",
	"hootPostID",
	"mc_eid",
	"mkclid",
	"mkt_tok",
	"msclkid",
	"pk_campaign",
	"pk_kwd",
	"sessionid",
	"sourceid",
	"utm_campaign",
	"utm_content",
	"utm_id",
	"utm_medium",
	"utm_source",
	"utm_term",
	"xpid",
}

// aggressively normalizes URL, for search indexing and matching. it is possible the URL won't be directly functional after this normalization
func NormalizeLossyURL(raw string) string {
	clean, err := purell.NormalizeURLString(raw, purell.FlagsUsuallySafeGreedy|purell.FlagRemoveDirectoryIndex|purell.FlagRemoveFragment|purell.FlagRemoveDuplicateSlashes|purell.FlagRemoveWWW|purell.FlagSortQuery)
	if err != nil {
		return raw
	}

	// remove tracking params
	u, err := url.Parse(clean)
	if err != nil {
		return clean
	}
	if u.RawQuery == "" {
		return clean
	}
	params := u.Query()

	// there is probably a more efficient way to do this
	for _, p := range trackingParams {
		if params.Has(p) {
			params.Del(p)
		}
	}
	u.RawQuery = params.Encode()
	return u.String()
}
