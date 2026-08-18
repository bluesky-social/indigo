package engine

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventProcessDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "automod_event_duration_sec",
	Help: "Total duration of automod event processing",
}, []string{"type"})

var eventProcessCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_event_processed",
	Help: "Number of events processed",
}, []string{"type"})

var eventErrorCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_event_errors",
	Help: "Number of events which failed processing",
}, []string{"type"})

var actionNewLabelCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_labels",
	Help: "Number of new labels persisted",
}, []string{"type", "val"})

var actionNewTagCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_tags",
	Help: "Number of new tags persisted",
}, []string{"type", "val"})

var actionNewFlagCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_flags",
	Help: "Number of new flags persisted",
}, []string{"type", "val"})

var actionNewReportCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_reports",
	Help: "Number of new flags persisted",
}, []string{"type"})

var actionNewTakedownCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_takedowns",
	Help: "Number of new takedowns",
}, []string{"type"})

var actionNewEscalationCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_escalations",
	Help: "Number of new subject escalations",
}, []string{"type"})

var actionNewAcknowledgeCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_new_action_acknowledges",
	Help: "Number of new subjects acknowledged",
}, []string{"type"})

var accountMetaFetches = promauto.NewCounter(prometheus.CounterOpts{
	Name: "automod_account_meta_fetches",
	Help: "Number of account metadata reads (API calls)",
})

var accountRelationshipFetches = promauto.NewCounter(prometheus.CounterOpts{
	Name: "automod_account_relationship_fetches",
	Help: "Number of account relationship reads (API calls)",
})

var blobDownloadCount = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "automod_blob_downloads",
	Help: "Number of blobs downloaded, by HTTP status code",
}, []string{"status"})

var blobDownloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name: "automod_blob_download_duration_sec",
	Help: "Duration of blob download attempts",
})
