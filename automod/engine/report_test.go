package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChildSafetyOtherReportDedupe(t *testing.T) {
	for _, tt := range []struct {
		name   string
		reason string
	}{
		{"generic-other", ReportReasonOther},
		{"rule-violation", ReportReasonViolation},
		{"csam", ReportReasonChildSafetyCSAM},
		{"unrecognized", "tools.ozone.report.defs#unrecognizedReason"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			eng := EngineTestFixture()
			subject := "did:plc:abc111"
			previous := []ModReport{{ReasonType: tt.reason}}
			got, err := eng.dedupeReportActions(ctx, subject, previous)
			require.NoError(t, err)
			require.Equal(t, previous, got)

			// A report in another category must not suppress child-safety review.
			reports := []ModReport{{ReasonType: "tools.ozone.report.defs#reasonChildSafetyOther"}}
			got, err = eng.dedupeReportActions(ctx, subject, reports)
			require.NoError(t, err)
			require.Equal(t, reports, got)

			// Repeating the same child-safety report is still deduplicated.
			got, err = eng.dedupeReportActions(ctx, subject, reports)
			require.NoError(t, err)
			require.Empty(t, got)
		})
	}
}
