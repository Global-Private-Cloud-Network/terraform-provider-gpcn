package l2segments

import (
	"encoding/json"
	"errors"
	"fmt"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToCreateL2Segment = "Unable to create GPCN L2 Segment"
	ErrSummaryUnableToGetL2Segment    = "Unable to get GPCN L2 Segment"
	ErrSummaryUnableToUpdateL2Segment = "Unable to update GPCN L2 Segment"
	ErrSummaryUnableToDeleteL2Segment = "Unable to delete GPCN L2 Segment"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient            = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetL2SegmentWithID    = "Unable to get GPCN L2 Segment with ID '%s'"
	ErrDetailUnableToUpdateL2SegmentWithID = "Unable to update GPCN L2 Segment with ID '%s'"
	ErrDetailUnableToDeleteL2SegmentWithID = "Unable to delete GPCN L2 Segment with ID '%s'"

	// The count is what the user acts on. The template repeats it at the end of
	// the sentence, where a reader looks for a number.
	ErrDetailAttachedNicCount = " (%d attached network interface(s))"

	ErrDetailNoSegmentIDInJob = "the create job reported no segment ID"
)

// Validation strings. The attribute name fills the template, so the sentence
// names the value the user must fix.
const ErrSummaryInvalidL2SegmentAttribute = "Invalid L2 segment %s"

// Warning strings for a segment the platform parked in the failed state
const (
	WarnSummaryL2SegmentFailed        = "L2 segment is in the failed state"
	WarnDetailL2SegmentFailed         = "L2 segment %s is in the failed state: %s. Destroy the segment and create it again."
	WarnDetailL2SegmentFailedNoReason = "L2 segment %s is in the failed state. Destroy the segment and create it again."
)

// FailedSegmentWarning explains a parked segment. The platform can park a row
// with no reason recorded. A sentence with an empty clause in it reads as a
// provider bug.
func FailedSegmentWarning(segmentID, failureReason string) diag.Diagnostic {
	if failureReason == "" {
		return diag.NewWarningDiagnostic(
			WarnSummaryL2SegmentFailed,
			fmt.Sprintf(WarnDetailL2SegmentFailedNoReason, segmentID),
		)
	}
	return diag.NewWarningDiagnostic(
		WarnSummaryL2SegmentFailed,
		fmt.Sprintf(WarnDetailL2SegmentFailed, segmentID, failureReason),
	)
}

// DeleteRefusalDetail renders a refused delete. Only the in-use refusal carries a
// count, and only that refusal is one the user can act on.
func DeleteRefusalDetail(err error) string {
	detail := err.Error()
	if !client.HasErrorCode(err, ErrorCodeL2SegmentInUse) {
		return detail
	}
	count, found := attachedNicCount(err)
	if !found {
		return detail
	}
	return detail + fmt.Sprintf(ErrDetailAttachedNicCount, count)
}

// attachedNicCount reads the count out of the refusal details. JSON numbers decode
// into any as float64, and a client that pre-decodes them can hand over a
// json.Number instead.
func attachedNicCount(err error) (int64, bool) {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return 0, false
	}
	raw, present := httpErr.Details[DetailKeyAttachedNicCount]
	if !present {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return int64(value), true
	case json.Number:
		parsed, parseErr := value.Int64()
		if parseErr != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
