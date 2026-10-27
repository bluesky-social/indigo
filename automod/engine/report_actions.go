package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// RuleMetadata identifies the rule revision responsible for a moderation action.
type RuleMetadata struct {
	Name    string `json:"name"`
	Version int64  `json:"version"`
}

// ReportAction selects report reasons on the subject of a takedown or label.
// Matching reports are actioned by the moderation event; other reports remain open.
type ReportAction struct {
	Rule  RuleMetadata
	Types []string
}

var (
	ruleNamePattern       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*/[a-z0-9]+(-[a-z0-9]+)*$`)
	reportFragmentPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
)

func (a ReportAction) validate() error {
	if !ruleNamePattern.MatchString(a.Rule.Name) || a.Rule.Version < 1 {
		return errors.New("report action requires a namespaced rule name and positive version")
	}
	if len(a.Types) == 0 {
		return errors.New("report action requires explicit report types")
	}
	for _, reason := range a.Types {
		nsid, fragment, ok := strings.Cut(reason, "#")
		if _, err := syntax.ParseNSID(nsid); err != nil || !ok || !reportFragmentPattern.MatchString(fragment) {
			return errors.New("report types must be fully qualified lexicon references")
		}
	}
	return nil
}

func (e *Effects) validateReportActions() error {
	groups := [][]ReportAction{e.accountTakedownReports, e.recordTakedownReports}
	for _, actions := range e.accountLabelReports {
		groups = append(groups, actions)
	}
	for _, actions := range e.recordLabelReports {
		groups = append(groups, actions)
	}
	for _, actions := range groups {
		for _, action := range actions {
			if err := action.validate(); err != nil {
				return fmt.Errorf("invalid report action: %w", err)
			}
		}
	}
	return nil
}

// TakedownAccountWithReportAction targets the given reasons when taking down the account.
func (c *AccountContext) TakedownAccountWithReportAction(action ReportAction) {
	c.effects.TakedownAccountWithReportAction(action)
}

// TakedownRecordWithReportAction targets the given reasons when taking down the record.
func (c *RecordContext) TakedownRecordWithReportAction(action ReportAction) {
	c.effects.TakedownRecordWithReportAction(action)
}

// AddAccountLabelWithReportAction targets reports only if this label is applied.
func (c *AccountContext) AddAccountLabelWithReportAction(val string, action ReportAction) {
	c.effects.AddAccountLabelWithReportAction(val, action)
}

// AddRecordLabelWithReportAction targets reports only if this label is applied.
func (c *RecordContext) AddRecordLabelWithReportAction(val string, action ReportAction) {
	c.effects.AddRecordLabelWithReportAction(val, action)
}

// TakedownAccountWithReportAction adds report targeting to the account takedown.
func (e *Effects) TakedownAccountWithReportAction(action ReportAction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.AccountTakedown = true
	action.Types = slices.Clone(action.Types)
	e.accountTakedownReports = append(e.accountTakedownReports, action)
}

// TakedownRecordWithReportAction adds report targeting to the record takedown.
func (e *Effects) TakedownRecordWithReportAction(action ReportAction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.RecordTakedown = true
	action.Types = slices.Clone(action.Types)
	e.recordTakedownReports = append(e.recordTakedownReports, action)
}

// AddAccountLabelWithReportAction adds a label and its associated report reasons.
func (e *Effects) AddAccountLabelWithReportAction(val string, action ReportAction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Contains(e.AccountLabels, val) {
		e.AccountLabels = append(e.AccountLabels, val)
	}
	if e.accountLabelReports == nil {
		e.accountLabelReports = make(map[string][]ReportAction)
	}
	action.Types = slices.Clone(action.Types)
	e.accountLabelReports[val] = append(e.accountLabelReports[val], action)
}

// AddRecordLabelWithReportAction adds a label and its associated report reasons.
func (e *Effects) AddRecordLabelWithReportAction(val string, action ReportAction) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Contains(e.RecordLabels, val) {
		e.RecordLabels = append(e.RecordLabels, val)
	}
	if e.recordLabelReports == nil {
		e.recordLabelReports = make(map[string][]ReportAction)
	}
	action.Types = slices.Clone(action.Types)
	e.recordLabelReports[val] = append(e.recordLabelReports[val], action)
}

func reportActionsForLabels(labels []string, actions map[string][]ReportAction) []ReportAction {
	var selected []ReportAction
	for _, label := range labels {
		selected = append(selected, actions[label]...)
	}
	return selected
}

func emitEventWithReports(ctx context.Context, client *atclient.APIClient, input *toolsozone.ModerationEmitEvent_Input, actions []ReportAction) (*toolsozone.ModerationDefs_ModEventView, error) {
	if len(actions) == 0 {
		return toolsozone.ModerationEmitEvent(ctx, client, input)
	}
	var types []string
	var rules []RuleMetadata
	for _, action := range actions {
		types = append(types, action.Types...)
		if !slices.Contains(rules, action.Rule) {
			rules = append(rules, action.Rule)
		}
	}
	automated := true
	input.ReportAction = &toolsozone.ModerationEmitEvent_ReportAction{
		Types: dedupeStrings(types), IsAutomated: &automated,
	}
	var meta any = struct {
		Source string         `json:"source"`
		Rules  []RuleMetadata `json:"rules"`
	}{Source: "automod", Rules: rules}
	input.ModTool = &toolsozone.ModerationDefs_ModTool{Name: "automod", Meta: &meta}
	out, err := toolsozone.ModerationEmitEvent(ctx, client, input)
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		apiErr.Name == "InvalidRequest" && apiErr.Message == "No matching reports found for the specified report types on this subject" {
		// This response rolls back the event and permits retrying without report selection.
		retry := *input
		retry.ReportAction = nil
		return toolsozone.ModerationEmitEvent(ctx, client, &retry)
	}
	return out, err
}
