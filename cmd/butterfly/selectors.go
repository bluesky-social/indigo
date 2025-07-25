// Package main provides query patterns for selecting content to sync and retain
package main

import (
	"encoding/json"
	"fmt"
)

// SelectorDoc represents a complete selector configuration with selection rules and retention policies
type SelectorDoc struct {
	Selectors []Selector `json:"select"`
	Retainers Retainer   `json:"retain"`
}

// Selector defines a rule for selecting content based on a where clause and assigns it a tag
type Selector struct {
	Where WhereClause `json:"where"`
	Tag   string      `json:"tag"`
}

// WhereClause specifies the criteria for selecting content
type WhereClause struct {
	// Repo selection fields
	Repo       string `json:"repo,omitempty"`
	Collection string `json:"collection,omitempty"`
	Attr       string `json:"attr,omitempty"`

	// Service selection fields
	Service    string            `json:"service,omitempty"`
	Method     string            `json:"method,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	Pagination map[string]string `json:"pagination,omitempty"`
}

// Retainer maps tags to their retention policies
// Format: tag -> collection pattern -> retention policy
type Retainer map[string]map[string]string

// String returns a string representation of the SelectorDoc
func (s SelectorDoc) String() string {
	return fmt.Sprintf("selectors=%v retain=%v", s.Selectors, s.Retainers)
}

// Validate checks if the SelectorDoc is valid
func (s SelectorDoc) Validate() error {
	if len(s.Selectors) == 0 {
		return fmt.Errorf("no selectors defined")
	}

	tags := make(map[string]bool)
	for i, sel := range s.Selectors {
		if err := sel.Validate(); err != nil {
			return fmt.Errorf("selector[%d]: %w", i, err)
		}
		if tags[sel.Tag] {
			return fmt.Errorf("duplicate tag: %s", sel.Tag)
		}
		tags[sel.Tag] = true
	}

	// Validate that all retainer tags exist in selectors
	for tag := range s.Retainers {
		if !tags[tag] {
			return fmt.Errorf("retainer references unknown tag: %s", tag)
		}
	}

	return nil
}

// IsRepo returns true if this selector targets a repository
func (s Selector) IsRepo() bool {
	return s.Where.Repo != "" && s.Where.Collection == "" && s.Where.Attr == ""
}

// IsRepoRecord returns true if this selector targets specific records in a repository
func (s Selector) IsRepoRecord() bool {
	return s.Where.Repo != "" && s.Where.Collection != "" && s.Where.Attr != ""
}

// IsService returns true if this selector targets a service endpoint
func (s Selector) IsService() bool {
	return s.Where.Service != "" && s.Where.Method != "" && s.Where.Attr != ""
}

// Type returns the type of selector as a string
func (s Selector) Type() string {
	switch {
	case s.IsRepo():
		return "repo"
	case s.IsRepoRecord():
		return "repo_record"
	case s.IsService():
		return "service"
	default:
		return "invalid"
	}
}

// Validate checks if the selector is valid
func (s Selector) Validate() error {
	if s.Tag == "" {
		return fmt.Errorf("missing tag")
	}

	if !s.IsRepo() && !s.IsRepoRecord() && !s.IsService() {
		return fmt.Errorf("invalid where clause: must specify either repo, repo+collection+attr, or service+method+attr")
	}

	return nil
}

// String returns a string representation of the Selector
func (s Selector) String() string {
	return fmt.Sprintf("tag=%s,%s", s.Tag, s.Where)
}

// String returns a string representation of the WhereClause
func (w WhereClause) String() string {
	switch {
	case w.Repo != "" && w.Collection != "" && w.Attr != "":
		return fmt.Sprintf("where=at://%s/%s/*#%s", w.Repo, w.Collection, w.Attr)
	case w.Repo != "":
		return fmt.Sprintf("where=at://%s", w.Repo)
	case w.Service != "" && w.Method != "" && w.Attr != "":
		return fmt.Sprintf("where=https://%s/_xrpc/%s/*#%s", w.Service, w.Method, w.Attr)
	default:
		return "where=(invalid)"
	}
}

// ParseSelectorDoc parses a JSON selector document
func ParseSelectorDoc(data []byte) (*SelectorDoc, error) {
	var doc SelectorDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse selector doc: %w", err)
	}

	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("invalid selector doc: %w", err)
	}

	return &doc, nil
}
