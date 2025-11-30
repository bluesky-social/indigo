package lexlint

import (
	"fmt"
	"log/slog"
	"reflect"
	"sort"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func BreakingChanges(before, after *lexicon.SchemaFile) []LintIssue {
	return breakingMaps(syntax.NSID(before.ID), before.Defs, after.Defs)
}

func breakingMaps(nsid syntax.NSID, localMap, remoteMap map[string]lexicon.SchemaDef) []LintIssue {
	issues := []LintIssue{}

	// TODO: maybe only care about the intersection of keys, not union?
	keyMap := map[string]bool{}
	for k := range localMap {
		keyMap[k] = true
	}
	for k := range remoteMap {
		keyMap[k] = true
	}
	keys := []string{}
	for k := range keyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// NOTE: adding or removing an entire definition or sub-object doesn't break anything
		local, ok := localMap[k]
		if !ok {
			continue
		}
		remote, ok := remoteMap[k]
		if !ok {
			continue
		}

		nestIssues := breakingDefs(nsid, k, local, remote)
		if len(nestIssues) > 0 {
			issues = append(issues, nestIssues...)
		}
	}

	return issues
}

func breakingDefs(nsid syntax.NSID, name string, local, remote lexicon.SchemaDef) []LintIssue {
	issues := []LintIssue{}

	// NOTE: in some situations this sort of change might actually be allowed?
	if reflect.TypeOf(local) != reflect.TypeOf(remote) {
		issues = append(issues, LintIssue{
			NSID:            nsid,
			LintLevel:       "error",
			LintName:        "type-change",
			LintDescription: "schema definition type changed",
			Message:         fmt.Sprintf("schema type changed (%s): %T != %T", name, local, remote),
		})
		return issues
	}

	switch l := local.Inner.(type) {
	case lexicon.SchemaRecord:
		slog.Debug("checking record", "name", name, "nsid", nsid)
		r := remote.Inner.(lexicon.SchemaRecord)
		if l.Key != r.Key {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "record-key-type",
				LintDescription: "record key type changed",
				Message:         fmt.Sprintf("schema type changed (%s): %s != %s", name, l.Key, r.Key),
			})
		}
		issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: l.Record}, lexicon.SchemaDef{Inner: r.Record})...)
	case lexicon.SchemaQuery:
		r := remote.Inner.(lexicon.SchemaQuery)
		// TODO: situation where overall parameters added/removed, and required fields involved
		if l.Parameters != nil && r.Parameters != nil {
			issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: *l.Parameters}, lexicon.SchemaDef{Inner: *r.Parameters})...)
		}
		// TODO: situation where output requirement changes
		if l.Output != nil && r.Output != nil {
			issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: *l.Output}, lexicon.SchemaDef{Inner: *r.Output})...)
		}
		// TODO: do Errors matter?
	case lexicon.SchemaProcedure:
		r := remote.Inner.(lexicon.SchemaProcedure)
		// TODO: situation where overall parameters added/removed, and required fields involved
		if l.Parameters != nil && r.Parameters != nil {
			issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: *l.Parameters}, lexicon.SchemaDef{Inner: *r.Parameters})...)
		}
		// TODO: situation where output requirement changes
		if l.Input != nil && r.Input != nil {
			issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: *l.Input}, lexicon.SchemaDef{Inner: *r.Input})...)
		}
		// TODO: situation where output requirement changes
		if l.Output != nil && r.Output != nil {
			issues = append(issues, breakingDefs(nsid, name, lexicon.SchemaDef{Inner: *l.Output}, lexicon.SchemaDef{Inner: *r.Output})...)
		}
		// TODO: do Errors matter?
	// TODO: lexicon.SchemaSubscription (and SchemaMessage)
	case lexicon.SchemaBody:
		r := remote.Inner.(lexicon.SchemaBody)
		if l.Encoding != r.Encoding {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "body-encoding",
				LintDescription: "API endpoint body content type (encoding) changed",
				Message:         fmt.Sprintf("body encoding changed (%s): %s != %s", name, l.Encoding, r.Encoding),
			})
		}
		if l.Schema != nil && r.Schema != nil {
			issues = append(issues, breakingDefs(nsid, name, *l.Schema, *r.Schema)...)
		}
	case lexicon.SchemaBoolean:
		r := remote.Inner.(lexicon.SchemaBoolean)
		// NOTE: default can change safely
		if !eqOptBool(l.Const, r.Const) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "const-value",
				LintDescription: "schema const value change",
				Message:         fmt.Sprintf("const value changed (%s): %v != %v", name, l.Const, r.Const),
			})
		}
	case lexicon.SchemaInteger:
		r := remote.Inner.(lexicon.SchemaInteger)
		// NOTE: default can change safely
		if !eqOptInt(l.Const, r.Const) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "const-value",
				LintDescription: "schema const value change",
				Message:         fmt.Sprintf("const value changed (%s): %v != %v", name, l.Const, r.Const),
			})
		}
		sort.Ints(l.Enum)
		sort.Ints(r.Enum)
		if !reflect.DeepEqual(l.Enum, r.Enum) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "enum-values",
				LintDescription: "schema enum values change",
				Message:         fmt.Sprintf("integer enum value changed (%s)", name),
			})
		}
		if !eqOptInt(l.Minimum, r.Minimum) || !eqOptInt(l.Maximum, r.Maximum) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "integer-range",
				LintDescription: "schema min/max values change",
				Message:         fmt.Sprintf("integer min/max values changed (%s)", name),
			})
		}
	case lexicon.SchemaString:
		r := remote.Inner.(lexicon.SchemaString)
		// NOTE: default can change safely
		if !eqOptString(l.Const, r.Const) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "const-value",
				LintDescription: "schema const value change",
				Message:         fmt.Sprintf("const value changed (%s)", name),
			})
		}
		sort.Strings(l.Enum)
		sort.Strings(r.Enum)
		if !reflect.DeepEqual(l.Enum, r.Enum) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "enum-values",
				LintDescription: "schema enum values change",
				Message:         fmt.Sprintf("string enum value changed (%s)", name),
			})
		}
		// NOTE: known values can change safely
		if !eqOptInt(l.MinLength, r.MinLength) || !eqOptInt(l.MaxLength, r.MaxLength) || !eqOptInt(l.MinGraphemes, r.MinGraphemes) || !eqOptInt(l.MaxGraphemes, r.MaxGraphemes) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "string-length",
				LintDescription: "string min/max length change",
				Message:         fmt.Sprintf("string min/max length change (%s)", name),
			})
		}
		if !eqOptString(l.Format, r.Format) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "string-format",
				LintDescription: "string format change",
				Message:         fmt.Sprintf("string format changed (%s)", name),
			})
		}
	case lexicon.SchemaBytes:
		r := remote.Inner.(lexicon.SchemaBytes)
		if !eqOptInt(l.MinLength, r.MinLength) || !eqOptInt(l.MaxLength, r.MaxLength) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "bytes-length",
				LintDescription: "bytes min/max length change",
				Message:         fmt.Sprintf("bytes min/max length change (%s)", name),
			})
		}
	case lexicon.SchemaCIDLink:
		// pass
	case lexicon.SchemaArray:
		r := remote.Inner.(lexicon.SchemaArray)
		if !eqOptInt(l.MinLength, r.MinLength) || !eqOptInt(l.MaxLength, r.MaxLength) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "array-length",
				LintDescription: "array min/max length change",
				Message:         fmt.Sprintf("array min/max length change (%s)", name),
			})
		}
		issues = append(issues, breakingDefs(nsid, name, l.Items, r.Items)...)
	case lexicon.SchemaObject:
		r := remote.Inner.(lexicon.SchemaObject)
		sort.Strings(l.Required)
		sort.Strings(r.Required)
		if !reflect.DeepEqual(l.Required, r.Required) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "object-required",
				LintDescription: "change in which fields are required",
				Message:         fmt.Sprintf("required fields change (%s)", name),
			})
		}
		sort.Strings(l.Nullable)
		sort.Strings(r.Nullable)
		if !reflect.DeepEqual(l.Nullable, r.Nullable) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "object-nullable",
				LintDescription: "change in which fields are nullable",
				Message:         fmt.Sprintf("nullable fields change (%s)", name),
			})
		}
		issues = append(issues, breakingMaps(nsid, l.Properties, r.Properties)...)
	case lexicon.SchemaBlob:
		r := remote.Inner.(lexicon.SchemaBlob)
		sort.Strings(l.Accept)
		sort.Strings(r.Accept)
		if !reflect.DeepEqual(l.Accept, r.Accept) {
			// TODO: how strong of a warning should this be?
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "blob-accept",
				LintDescription: "change in blob accept (content-type)",
				Message:         fmt.Sprintf("blob accept change (%s)", name),
			})
		}
		if !eqOptInt(l.MaxSize, r.MaxSize) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "blob-size",
				LintDescription: "blob maximum size change",
				Message:         fmt.Sprintf("blob max size change (%s)", name),
			})
		}
	case lexicon.SchemaParams:
		r := remote.Inner.(lexicon.SchemaParams)
		sort.Strings(l.Required)
		sort.Strings(r.Required)
		if !reflect.DeepEqual(l.Required, r.Required) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "params-required",
				LintDescription: "change in which fields are required",
				Message:         fmt.Sprintf("required fields change (%s)", name),
			})
		}
		issues = append(issues, breakingMaps(nsid, l.Properties, r.Properties)...)
	case lexicon.SchemaRef:
		r := remote.Inner.(lexicon.SchemaRef)
		if l.Ref != r.Ref {
			// NOTE: if the underlying schemas are the same this could be ok in some situations
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "ref-change",
				LintDescription: "change in referenced lexicon",
				Message:         fmt.Sprintf("ref change (%s): %s != %s", name, l.Ref, r.Ref),
			})
		}
	case lexicon.SchemaUnion:
		r := remote.Inner.(lexicon.SchemaUnion)
		if !eqOptBool(l.Closed, r.Closed) {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "union-open-closed",
				LintDescription: "can't change union between open and closed",
				Message:         fmt.Sprintf("union open/closed type changed (%s)", name),
			})
		}
		// check for refs changing in closed union
		if l.Closed != nil && *l.Closed {
			sort.Strings(l.Refs)
			sort.Strings(r.Refs)
			if !reflect.DeepEqual(l.Refs, r.Refs) {
				issues = append(issues, LintIssue{
					NSID:            nsid,
					LintLevel:       "error",
					LintName:        "union-closed-refs",
					LintDescription: "closed unions can not have types (refs) change",
					Message:         fmt.Sprintf("closed union types (refs) changed (%s)", name),
				})
			}
		}
	case lexicon.SchemaUnknown, lexicon.SchemaPermissionSet, lexicon.SchemaToken:
		// pass
	default:
		slog.Warn("unhandled schema def type in breaking check", "type", reflect.TypeOf(local.Inner))
	}

	return issues
}

// helper to check if two optional (pointer) integers are equal/consistent
func eqOptInt(a, b *int) bool {
	if a == nil {
		return b == nil
	}
	if b == nil {
		return false
	}
	return *a == *b
}

func eqOptBool(a, b *bool) bool {
	if a == nil {
		return b == nil
	}
	if b == nil {
		return false
	}
	return *a == *b
}

func eqOptString(a, b *string) bool {
	if a == nil {
		return b == nil
	}
	if b == nil {
		return false
	}
	return *a == *b
}
