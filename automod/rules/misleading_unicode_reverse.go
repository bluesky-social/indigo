package rules

import (
	"strings"

	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/automod"
)

func MisleadingLinkUnicodeReversalPostRule(c *automod.RecordContext, post *appgndr.FeedPost) error {

	if !strings.Contains(post.Text, "\u202E") {
		return nil
	}

	c.AddRecordFlag("clickjack-unicode-reversed")
	return nil
}
