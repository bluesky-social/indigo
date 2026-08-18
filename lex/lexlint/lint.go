package lexlint

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type LintIssue struct {
	FilePath        string      `json:"file-path,omitempty"`
	NSID            syntax.NSID `json:"nsid,omitempty"`
	LintLevel       string      `json:"lint-level,omitempty"`
	LintName        string      `json:"lint-name,omitempty"`
	LintDescription string      `json:"lint-description,omitempty"`
	Message         string      `json:"message,omitempty"`
}

func LintSchemaFile(sf *lexicon.SchemaFile) []LintIssue {
	issues := []LintIssue{}

	nsid, err := syntax.ParseNSID(sf.ID)
	if err != nil {
		issues = append(issues, LintIssue{
			NSID:            syntax.NSID(sf.ID),
			LintLevel:       "error",
			LintName:        "invalid-nsid",
			LintDescription: "schema file declares NSID with invalid syntax",
			Message:         fmt.Sprintf("NSID string: %s", sf.ID),
		})
	}
	if nsid == "" {
		nsid = syntax.NSID(sf.ID)
	}
	if sf.Lexicon != 1 {
		issues = append(issues, LintIssue{
			NSID:            nsid,
			LintLevel:       "error",
			LintName:        "lexicon-version",
			LintDescription: "unsupported Lexicon language version",
			Message:         fmt.Sprintf("found version: %d", sf.Lexicon),
		})
		return issues
	}

	for defname, def := range sf.Defs {
		defiss := lintSchemaDef(nsid, defname, def)
		if len(defiss) > 0 {
			issues = append(issues, defiss...)
		}
	}

	return issues
}

func lintSchemaDef(nsid syntax.NSID, defname string, def lexicon.SchemaDef) []LintIssue {
	issues := []LintIssue{}

	// missing description issue, in case it is needed
	missingDesc := func() LintIssue {
		return LintIssue{
			NSID:            nsid,
			LintLevel:       "warn",
			LintName:        "missing-primary-description",
			LintDescription: "primary types (record, query, procedure, subscription, permission-set) should include a description",
			Message:         "primary type missing a description",
		}
	}

	if err := def.CheckSchema(); err != nil {
		issues = append(issues, LintIssue{
			NSID:            nsid,
			LintLevel:       "error",
			LintName:        "lexicon-schema",
			LintDescription: "basic structure schema checks (additional errors may be collapsed)",
			Message:         err.Error(),
		})
	}

	if err := CheckSchemaName(defname); err != nil {
		issues = append(issues, LintIssue{
			NSID:            nsid,
			LintLevel:       "warn",
			LintName:        "def-name-syntax",
			LintDescription: "definition name does not follow syntax guidance",
			Message:         fmt.Sprintf("%s: %s", err.Error(), defname),
		})
	}

	if nsid.Name() == "defs" && defname == "main" {
		issues = append(issues, LintIssue{
			NSID:            nsid,
			LintLevel:       "warn",
			LintName:        "defs-main-definition",
			LintDescription: "defs schemas should not have a 'main'",
			Message:         "defs schemas should not have a 'main'",
		})
	}

	switch def.Inner.(type) {
	// NOTE: not requiring description on permission-set
	case lexicon.SchemaRecord, lexicon.SchemaQuery, lexicon.SchemaProcedure, lexicon.SchemaSubscription:
		if defname != "main" {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "error",
				LintName:        "non-main-primary",
				LintDescription: "primary types (record, query, procedure, subscription, permission-set) must be 'main' definition",
				Message:         fmt.Sprintf("primary definition types must be 'main': %s", defname),
			})
		}
	}

	switch v := def.Inner.(type) {
	case lexicon.SchemaRecord:
		if v.Description == nil || *v.Description == "" {
			issues = append(issues, missingDesc())
		}
		reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: v.Record})
		if len(reciss) > 0 {
			issues = append(issues, reciss...)
		}
	case lexicon.SchemaQuery:
		if v.Description == nil || *v.Description == "" {
			issues = append(issues, missingDesc())
		}
		if v.Parameters != nil {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Parameters})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		if v.Output == nil {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "endpoint-output-undefined",
				LintDescription: "API endpoints should define an output (even if empty)",
				Message:         "missing output definition",
			})
		} else {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Output})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		// TODO: error names
	case lexicon.SchemaProcedure:
		if v.Description == nil || *v.Description == "" {
			issues = append(issues, missingDesc())
		}
		if v.Parameters != nil {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Parameters})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		if v.Input != nil {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Input})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		if v.Output == nil {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "endpoint-output-undefined",
				LintDescription: "API endpoints should define an output (even if empty)",
				Message:         "missing output definition",
			})
		} else {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Output})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		// TODO: error names
	case lexicon.SchemaSubscription:
		if v.Description == nil || *v.Description == "" {
			issues = append(issues, missingDesc())
		}
		if v.Parameters != nil {
			reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: *v.Parameters})
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		// TODO: v.Message.Schema must only have local references (same file)
		reciss := lintSchemaRecursive(nsid, lexicon.SchemaDef{Inner: v.Message.Schema})
		if len(reciss) > 0 {
			issues = append(issues, reciss...)
		}
		if len(v.Message.Schema.Refs) == 0 {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "subscription-no-messages",
				LintDescription: "no subscription message types defined",
				Message:         "no subscription message types defined",
			})
		}
	case lexicon.SchemaPermissionSet:
		if v.Title == nil || *v.Title == "" {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "permissionset-no-title",
				LintDescription: "permission sets should include a title",
				Message:         "missing title",
			})
		}
		// TODO: missing detail
		// TODO: translated descriptions?
		if len(v.Permissions) == 0 {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "permissionset-no-permissions",
				LintDescription: "permission sets should define at least one permission",
				Message:         "empty permission set",
			})
		}
		for _, perm := range v.Permissions {
			// TODO: any lints on permissions?
			_ = perm
		}
	case lexicon.SchemaPermission, lexicon.SchemaBoolean, lexicon.SchemaInteger, lexicon.SchemaString, lexicon.SchemaBytes, lexicon.SchemaCIDLink, lexicon.SchemaArray, lexicon.SchemaObject, lexicon.SchemaBlob, lexicon.SchemaToken, lexicon.SchemaRef, lexicon.SchemaUnion, lexicon.SchemaUnknown:
		reciss := lintSchemaRecursive(nsid, def)
		if len(reciss) > 0 {
			issues = append(issues, reciss...)
		}
	default:
		slog.Info("no lint rules for top-level schema definition type", "type", fmt.Sprintf("%T", def.Inner))
	}
	return issues
}

func lintSchemaRecursive(nsid syntax.NSID, def lexicon.SchemaDef) []LintIssue {
	issues := []LintIssue{}

	switch v := def.Inner.(type) {
	case lexicon.SchemaBoolean:
		// TODO: default true
		// TODO: both default and const
	case lexicon.SchemaInteger:
		// TODO: both default and const
	case lexicon.SchemaString:
		// TODO: format and length limits
		// TODO: grapheme limit set, and maxlen either too low or not set
		// TODO: format=handle strings within an record type
		if v.MaxLength != nil && *v.MaxLength > 20*1024 {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "large-string",
				LintDescription: "string field with large maximum size (use blobs instead)",
				Message:         "large max length",
			})
		}
		if v.Format == nil && v.MaxLength == nil && v.MaxGraphemes == nil {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "unlimited-string",
				LintDescription: "string field with no format or maximum size",
				Message:         "no max length",
			})
		}
		for _, val := range v.KnownValues {
			if strings.HasPrefix(val, "#") {
				issues = append(issues, LintIssue{
					NSID:            nsid,
					LintLevel:       "warn",
					LintName:        "known-string-local-ref",
					LintDescription: "string knownValues entry which seems to be a local reference",
					Message:         fmt.Sprintf("possible local ref: %s", val),
				})
			}
		}
	case lexicon.SchemaBytes:
		if v.MaxLength == nil {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "unlimited-bytes",
				LintDescription: "bytes field with no maximum size",
				Message:         "no max length",
			})
		}
		if v.MaxLength != nil && *v.MaxLength > 20*1024 {
			// TODO: limit this to 'record' schemas?
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "large-bytes",
				LintDescription: "bytes field with large maximum size (use blobs instead)",
				Message:         "large max length",
			})
		}
	case lexicon.SchemaCIDLink:
		// pass
	case lexicon.SchemaBlob:
		// pass
	case lexicon.SchemaArray:
		reciss := lintSchemaRecursive(nsid, v.Items)
		if len(reciss) > 0 {
			issues = append(issues, reciss...)
		}
	case lexicon.SchemaObject:
		// NOTE: CheckSchema already verifies that nullable and required are valid against property keys
		for fieldName, propdef := range v.Properties {
			reciss := lintSchemaRecursive(nsid, propdef)
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
			if err := CheckSchemaName(fieldName); err != nil {
				issues = append(issues, LintIssue{
					NSID:            nsid,
					LintLevel:       "warn",
					LintName:        "field-name-syntax",
					LintDescription: "field name does not follow syntax guidance",
					Message:         fmt.Sprintf("%s: %s", err.Error(), fieldName),
				})
			}
		}
		for _, k := range v.Nullable {
			if !slices.Contains(v.Required, k) {
				issues = append(issues, LintIssue{
					NSID:            nsid,
					LintLevel:       "warn",
					LintName:        "nullable-and-optional",
					LintDescription: "object properties should not be both optional and nullable",
					Message:         fmt.Sprintf("field is both nullable and optional: %s", k),
				})
			}
		}
	case lexicon.SchemaParams:
		// NOTE: CheckSchema already verifies that required are valid against property keys
		for fieldName, propdef := range v.Properties {
			reciss := lintSchemaRecursive(nsid, propdef)
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
			if err := CheckSchemaName(fieldName); err != nil {
				issues = append(issues, LintIssue{
					NSID:            nsid,
					LintLevel:       "warn",
					LintName:        "field-name-syntax",
					LintDescription: "field name does not follow syntax guidance",
					Message:         fmt.Sprintf("%s: %s", err.Error(), fieldName),
				})
			}
		}
	case lexicon.SchemaToken:
		if v.Description == nil || *v.Description == "" {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "undescribed-token",
				LintDescription: "token without description field",
				Message:         "empty description",
			})
		}
	case lexicon.SchemaRef:
		// TODO: resolve? locally vs globally?
	case lexicon.SchemaUnion:
		// TODO: open vs closed?
		// TODO: check that refs actually resolve?
	case lexicon.SchemaUnknown:
		// pass
	case lexicon.SchemaBody:
		if v.Schema != nil {
			// NOTE: CheckSchema already verified that v.Schema is an object, ref, or union
			reciss := lintSchemaRecursive(nsid, *v.Schema)
			if len(reciss) > 0 {
				issues = append(issues, reciss...)
			}
		}
		if v.Encoding == "" {
			issues = append(issues, LintIssue{
				NSID:            nsid,
				LintLevel:       "warn",
				LintName:        "unspecified-encoding",
				LintDescription: "body encoding not specified",
				Message:         "missing encoding",
			})
		}
	default:
		slog.Info("no lint rules for recursive schema type", "type", fmt.Sprintf("%T", def.Inner), "nsid", nsid)
	}

	return issues
}
