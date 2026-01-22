package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/labeling"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type LabelProcessor struct {
	logger             *slog.Logger
	db                 *gorm.DB
	events             *EventManager
	idDir              identity.Directory
	cursorSaveInterval time.Duration

	labelerDID string
	serviceURL *string

	lastSeq atomic.Int64
}

func (lp *LabelProcessor) Run(ctx context.Context) error {
	lp.logger.Info("starting label processor")

	go lp.runCursorSaver(ctx)

	var retries int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var labeler models.Labeler
		if err := lp.db.First(&labeler, "did = ?", lp.labelerDID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				lp.logger.Info("labeler removed, stopping processor")
				return nil
			}
			if ctx.Err() == nil {
				lp.logger.Error("failed to load labeler state", "error", err)
			}
			time.Sleep(backoff(retries, 60))
			retries++
			continue
		}

		if labeler.ServiceURL == nil {
			lp.logger.Warn("labeler service URL not yet resolved, waiting")
			time.Sleep(backoff(retries, 60))
			retries++
			continue
		}

		if labeler.RetryAfter > 0 && time.Now().Unix() < labeler.RetryAfter {
			waitTime := time.Until(time.Unix(labeler.RetryAfter, 0))
			lp.logger.Debug("waiting for retry backoff", "wait", waitTime)
			time.Sleep(waitTime)
			continue
		}

		if lp.serviceURL == nil || *labeler.ServiceURL != *lp.serviceURL {
			lp.logger.Info("service URL changed, reconnecting", "old", lp.serviceURL, "new", labeler.ServiceURL)
			lp.serviceURL = labeler.ServiceURL
		}

		cursor := labeler.Cursor
		lp.lastSeq.Store(cursor)

		err := lp.runSubscriber(ctx, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lp.logger.Warn("label subscription failed", "error", err)
			if err := lp.handleSubscriptionError(ctx, err); err != nil {
				if ctx.Err() == nil {
					lp.logger.Error("failed to handle subscription error", "error", err)
				}
			}
			retries++
			time.Sleep(backoff(retries, 60))
		} else {
			retries = 0
			if err := lp.clearError(ctx); err != nil {
				if ctx.Err() == nil {
					lp.logger.Error("failed to clear error state", "error", err)
				}
			}
		}
	}
}

func (lp *LabelProcessor) runSubscriber(ctx context.Context, cursor int64) error {
	if lp.serviceURL == nil {
		return fmt.Errorf("service URL is nil")
	}

	u, err := url.Parse(*lp.serviceURL)
	if err != nil {
		return fmt.Errorf("invalid service URL: %w", err)
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "xrpc/com.atproto.label.subscribeLabels"
	u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
	urlStr := u.String()

	lp.logger.Info("connecting to labeler", "url", urlStr, "cursor", cursor)

	headers := http.Header{}
	headers.Set("User-Agent", userAgent())
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, urlStr, headers)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	lp.logger.Info("connected to labeler")

	rsc := &events.RepoStreamCallbacks{
		LabelLabels: func(evt *comatproto.LabelSubscribeLabels_Labels) error {
			return lp.processLabels(ctx, evt)
		},
		RepoInfo: func(evt *comatproto.SyncSubscribeRepos_Info) error {
			// TODO: `RepoInfo ... SyncSubscribeRepos_Info` should be `LabelInfo ... LabelSubscribeLabels_Info`; see events/consumer.go:268
			lp.logger.Info("received label info event", "name", evt.Name, "message", evt.Message)
			return nil
		},
		Error: func(err *events.ErrorFrame) error {
			return fmt.Errorf("error frame: %s - %s", err.Error, err.Message)
		},
	}

	scheduler := parallel.NewScheduler(1, 100, "label_subscriber", rsc.EventHandler)
	return events.HandleRepoStream(ctx, conn, scheduler, lp.logger)
}

func (lp *LabelProcessor) runCursorSaver(ctx context.Context) {
	ticker := time.NewTicker(lp.cursorSaveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := lp.saveCursor(ctx); err != nil {
				if ctx.Err() == nil {
					lp.logger.Error("failed to save cursor", "error", err)
				}
			}
		}
	}
}

func (lp *LabelProcessor) saveCursor(ctx context.Context) error {
	seq := lp.lastSeq.Load()
	if seq == 0 {
		return nil
	}

	return lp.db.WithContext(ctx).Model(&models.Labeler{}).
		Where("did = ?", lp.labelerDID).
		Update("cursor", seq).Error
}

func (lp *LabelProcessor) processLabels(ctx context.Context, evt *comatproto.LabelSubscribeLabels_Labels) error {
	lp.logger.Debug("received labels", "count", len(evt.Labels), "seq", evt.Seq)

	lp.lastSeq.Store(evt.Seq)
	labelLastSeq.WithLabelValues(lp.labelerDID).Set(float64(evt.Seq))

	for _, label := range evt.Labels {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		labelEventsReceived.Inc()
		if err := lp.processLabel(ctx, label); err != nil {
			if ctx.Err() == nil {
				lp.logger.Error("failed to process label", "error", err, "uri", label.Uri)
			}
		} else {
			labelEventsProcessed.Inc()
		}
	}

	return nil
}

func (lp *LabelProcessor) processLabel(ctx context.Context, label *comatproto.LabelDefs_Label) error {
	if len(label.Sig) > 0 {
		srcDid, err := syntax.ParseDID(label.Src)
		if err != nil {
			lp.logger.Warn("invalid source DID in label", "src", label.Src, "error", err)
			return nil
		}

		ident, err := lp.idDir.LookupDID(ctx, srcDid)
		if err != nil {
			return fmt.Errorf("failed to resolve DID %s: %w", label.Src, err)
		}

		pubKey, err := ident.GetPublicKey("atproto_label")
		if err != nil {
			return fmt.Errorf("no atproto_label key for %s: %w", label.Src, err)
		}

		labelToVerify := labeling.FromLexicon(label)
		if err := labelToVerify.VerifySignature(pubKey); err != nil {
			return fmt.Errorf("signature verification failed for label from %s: %w", label.Src, err)
		}
		lp.logger.Debug("label signature verified", "src", label.Src, "uri", label.Uri)
	}

	createdAt, err := time.Parse(time.RFC3339, label.Cts)
	if err != nil {
		lp.logger.Warn("failed to parse label timestamp", "error", err, "timestamp", label.Cts)
		createdAt = time.Now()
	}

	isLive := time.Since(createdAt) < 1*time.Minute

	evt := &LabelEvt{
		Live:       isLive,
		LabelerDID: lp.labelerDID,
		Uri:        label.Uri,
		Val:        label.Val,
		Cts:        label.Cts,
		Src:        label.Src,
	}

	if label.Cid != nil {
		evt.Cid = *label.Cid
	}

	if label.Neg != nil && *label.Neg {
		evt.Neg = true
	}

	if label.Exp != nil {
		evt.Exp = *label.Exp
	}

	return lp.events.AddLabelEvent(ctx, evt)
}

func (lp *LabelProcessor) handleSubscriptionError(ctx context.Context, err error) error {
	var labeler models.Labeler
	if err := lp.db.WithContext(ctx).First(&labeler, "did = ?", lp.labelerDID).Error; err != nil {
		return err
	}

	retryAfter := time.Now().Add(backoff(labeler.RetryCount, 60) * 60)

	return lp.db.WithContext(ctx).Model(&models.Labeler{}).
		Where("did = ?", lp.labelerDID).
		Updates(map[string]any{
			"state":       "error",
			"error_msg":   err.Error(),
			"retry_count": labeler.RetryCount + 1,
			"retry_after": retryAfter.Unix(),
		}).Error
}

func (lp *LabelProcessor) clearError(ctx context.Context) error {
	return lp.db.WithContext(ctx).Model(&models.Labeler{}).
		Where("did = ?", lp.labelerDID).
		Updates(map[string]any{
			"state":       "active",
			"error_msg":   "",
			"retry_count": 0,
			"retry_after": 0,
		}).Error
}
