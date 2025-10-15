package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"golang.org/x/net/publicsuffix"
)

func defType(sd *lexicon.SchemaDef) (string, error) {
	switch sd.Inner.(type) {
	case lexicon.SchemaRecord:
		return "record", nil
	case lexicon.SchemaQuery:
		return "query", nil
	case lexicon.SchemaProcedure:
		return "procedure", nil
	case lexicon.SchemaSubscription:
		return "subscription", nil
	case lexicon.SchemaPermissionSet:
		return "permission-set", nil
	case lexicon.SchemaPermission:
		return "permission", nil
	case lexicon.SchemaNull:
		return "null", nil
	case lexicon.SchemaBoolean:
		return "boolean", nil
	case lexicon.SchemaInteger:
		return "integer", nil
	case lexicon.SchemaString:
		return "string", nil
	case lexicon.SchemaBytes:
		return "bytes", nil
	case lexicon.SchemaCIDLink:
		return "cid-link", nil
	case lexicon.SchemaArray:
		return "array", nil
	case lexicon.SchemaObject:
		return "object", nil
	case lexicon.SchemaBlob:
		return "blob", nil
	case lexicon.SchemaParams:
		return "params", nil
	case lexicon.SchemaToken:
		return "token", nil
	case lexicon.SchemaRef:
		return "ref", nil
	case lexicon.SchemaUnion:
		return "union", nil
	case lexicon.SchemaUnknown:
		return "unknown", nil
	default:
		return "", fmt.Errorf("unhandled schema type: %T", sd.Inner)
	}
}

func isCompoundDef(sd *lexicon.SchemaDef) bool {
	switch sd.Inner.(type) {
	case lexicon.SchemaRecord, lexicon.SchemaQuery, lexicon.SchemaProcedure, lexicon.SchemaSubscription, lexicon.SchemaArray, lexicon.SchemaObject, lexicon.SchemaUnion:
		return true
	default:
		return false
	}
}

func nsidPkgName(nsid syntax.NSID) string {
	domain := strings.ToLower(nsid.Authority())
	reg, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return "FAIL"
	}
	parts := strings.Split(reg, ".")
	slices.Reverse(parts)

	return strings.Join(parts, "")
}

func nsidBaseName(nsid syntax.NSID) string {
	domain := strings.ToLower(nsid.Authority())
	reg, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return "FAIL"
	}
	rem := domain[0 : len(domain)-len(reg)]
	parts := strings.Split(rem, ".")
	slices.Reverse(parts)
	parts = append(parts, nsid.Name())
	for i := range parts {
		parts[i] = strings.Title(parts[i])
	}
	return strings.Join(parts, "")
}
