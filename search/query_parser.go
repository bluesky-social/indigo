package search

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DID is not currently being used presently
type DidHandle struct {
	DID    string
	Handle string
}

type SearchQuery struct {
	FromUser    *DidHandle
	QueryString string
	Offset      int
	Count       int
}

var /* const */ FromOperatorRegexp = regexp.MustCompile(`(?i)\bfrom:([\w\.]+)`)
var /* const */ SearchQueryOffsetDefault = 0
var /* const */ SearchQueryOffsetMin = 0
var /* const */ SearchQueryCountDefault = 30
var /* const */ SearchQueryCountMax = 100

func paramsToSearchQuery(queryString string, offsetParam string, countParam string) (*SearchQuery, error) {
	var searchQuery SearchQuery

	queryString = strings.TrimSpace(queryString)

	// If the searchQuery contains 'from:username.foo.tld',
	// extract username.foo.tld, and remove the entire operator token.
	// If there are multiple from: matches, ignore.
	fromIdentifier := ""
	allMatches := FromOperatorRegexp.FindAllStringSubmatch(queryString, -1)
	if len(allMatches) == 1 {
		matches := allMatches[0]
		fromIdentifier = matches[1]
		// TODO can update regex to allow : and populate DID based on fromIdentifier
		searchQuery.FromUser = &DidHandle{
			Handle: fromIdentifier,
			DID:    "",
		}
		queryString = strings.TrimSpace(
			FromOperatorRegexp.ReplaceAllString(queryString, ""),
		)
	}

	if queryString == "" && fromIdentifier == "" {
		return nil, errors.New("query string cannot be empty")
	}

	searchQuery.QueryString = queryString

	offset := SearchQueryOffsetDefault
	if offsetParam := strings.TrimSpace(offsetParam); offsetParam != "" {
		v, err := strconv.Atoi(offsetParam)
		if err != nil {
			return nil, errors.New(
				fmt.Sprintf("Invalid value for 'offset': %s", err),
			)
		}
		offset = v
	}
	if offset < SearchQueryOffsetMin {
		offset = SearchQueryOffsetMin
	}
	searchQuery.Offset = offset

	count := SearchQueryCountDefault
	if countParam := strings.TrimSpace(countParam); countParam != "" {
		v, err := strconv.Atoi(countParam)
		if err != nil {
			return nil, errors.New(
				fmt.Sprintf("Invalid value for 'count': %s", err),
			)
		}
		count = v
	}
	if count > SearchQueryCountMax {
		count = SearchQueryCountDefault
	}
	searchQuery.Count = count

	return &searchQuery, nil
}
