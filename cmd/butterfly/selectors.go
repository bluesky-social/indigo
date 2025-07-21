/*
Query patterns for selecting content to sync and retain
*/

package main

import "fmt"

type SelectorDoc struct {
	Selectors []Selector `json:"select"`
	Retainers Retainer   `json:"retain"`
}

type Selector struct {
	Where WhereClause `json:"where"`
	Tag   string      `json:"tag"`
}

type WhereClause struct {
	Repo       string            `json:"repo"`
	Collection string            `json:"collection"`
	Attr       string            `json:"attr"`
	Service    string            `json:"service"`
	Method     string            `json:"method"`
	Params     map[string]string `json:"params"`
	Pagination map[string]string `json:"pagination"`
}

type Retainer map[string]map[string]string

func (self SelectorDoc) String() string {
	return fmt.Sprintf("%s retain=%s", self.Selectors, self.Retainers)
}

func (self Selector) String() string {
	if self.Tag == "" {
		return "(Invalid selector)"
	}
	return fmt.Sprintf("%s,tag=%s", self.Where, self.Tag)
}

func (self WhereClause) String() string {
	if self.Repo != "" && self.Collection != "" && self.Attr != "" {
		return fmt.Sprintf("where=at://%s/%s/*#%s", self.Repo, self.Collection, self.Attr)
	}
	if self.Repo != "" {
		return fmt.Sprintf("where=at://%s", self.Repo)
	}
	if self.Service != "" && self.Method != "" && self.Attr != "" {
		return fmt.Sprintf("where=https://%s/_xrpc/%s/*#%s", self.Service, self.Method, self.Attr)
	}
	return "where=(Invalid clause)"
}
