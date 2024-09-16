package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

type MentionChecker struct {
	slackWebhookURL string
	mentionDIDs     []syntax.DID
	logger          *slog.Logger
	directory       identity.Directory
	minimumWords    int
}

func (mc *MentionChecker) ProcessPost(ctx context.Context, did syntax.DID, rkey syntax.RecordKey, post appbsky.FeedPost) error {
	mc.logger.Debug("processing post record", "did", did, "rkey", rkey)

	if mc.minimumWords > 0 {
		words := strings.Split(post.Text, " ")
		if len(words) < mc.minimumWords {
			return nil
		}
	}

	for _, facet := range post.Facets {
		for _, feature := range facet.Features {
			mention := feature.RichtextFacet_Mention
			if mention == nil {
				continue
			}
			for _, d := range mc.mentionDIDs {
				if mention.Did == d.String() {
					mc.logger.Info("found mention", "target", d, "author", did, "rkey", rkey)
					targetIdent, err := mc.directory.LookupDID(ctx, syntax.DID(mention.Did))
					if err != nil {
						return err
					}
					authorIdent, err := mc.directory.LookupDID(ctx, did)
					if err != nil {
						return err
					}
					msg := fmt.Sprintf("Mention of `@%s` by `@%s` (<https://bsky.app/profile/%s/post/%s|post link>):\n```%s```", targetIdent.Handle, authorIdent.Handle, did, rkey, post.Text)
					if post.Embed != nil && (post.Embed.EmbedImages != nil || post.Embed.EmbedRecordWithMedia != nil || post.Embed.EmbedRecord != nil || post.Embed.EmbedExternal != nil) {
						msg += "\n(post also contains an embed/quote/media)"
					}
					return sendSlackMsg(ctx, msg, mc.slackWebhookURL)
				}
			}
		}
	}
	return nil
}

func notifyMentions(cctx *cli.Context) error {
	ctx := context.Background()
	logger := configLogger(cctx, os.Stdout)
	relayHost := cctx.String("relay-host")
	minimumWords := cctx.Int("minimum-words")

	mentionDIDs := []syntax.DID{}
	for _, raw := range strings.Split(cctx.String("mention-dids"), ",") {
		did, err := syntax.ParseDID(raw)
		if err != nil {
			return err
		}
		mentionDIDs = append(mentionDIDs, did)
	}

	checker := MentionChecker{
		slackWebhookURL: cctx.String("slack-webhook-url"),
		mentionDIDs:     mentionDIDs,
		logger:          logger,
		directory:       identity.DefaultDirectory(),
		minimumWords:    minimumWords,
	}

	logger.Info("beemo mention checker starting up...", "relayHost", relayHost, "mentionDIDs", mentionDIDs)

	// can flip this bool to false to prevent spamming slack channel on startup
	if true {
		err := sendSlackMsg(ctx, fmt.Sprintf("beemo booting, looking for account mentions: `%s`", mentionDIDs), checker.slackWebhookURL)
		if err != nil {
			return err
		}
	}

	return RunFirehoseConsumer(ctx, logger, relayHost, checker.ProcessPost)
}
