package vpcs

import (
	"errors"
	"fmt"
	"strings"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateVpc       = "Unable to create GPCN VPC"
	ErrSummaryUnableToGetVpc          = "Unable to get GPCN VPC"
	ErrSummaryUnableToUpdateVpc       = "Unable to update GPCN VPC"
	ErrSummaryUnableToDeleteVpc       = "Unable to delete GPCN VPC"
	ErrSummaryInvalidVpcCidr          = "Invalid VPC CIDR"
	ErrSummaryVpcCidrOverlap          = "VPC CIDR overlaps an existing VPC"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient      = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetVpcWithID    = "Unable to get GPCN VPC with ID '%s'"
	ErrDetailUnableToUpdateVpcWithID = "Unable to update GPCN VPC with ID '%s'"
	ErrDetailUnableToDeleteVpcWithID = "Unable to delete GPCN VPC with ID '%s'"

	// The first %s carries the API sentence. The reader needs the names behind
	// the refusal, and the one knob that clears it.
	ErrDetailVpcCidrOverlap       = "%s Overlapping VPCs: %s. Set acknowledge_overlap = true to proceed."
	ErrDetailVpcCidrOverlapHidden = "%s Set acknowledge_overlap = true to proceed."
	// The API counts children in two populations. The reader deletes the first
	// and waits for the second.
	ErrDetailVpcNotEmpty = "%s Blockers: %s. In flight: %s."
)

// The messages GPCN's own super-CIDR refinement raises, byte for byte
// (src/components/vpc/vpc.validation.ts:31,38,45). The plan and the API refuse
// the same strings only while these agree.
const (
	ErrDetailVpcCidrShape      = "must be a valid IPv4 CIDR whose address is the network address (e.g. 10.50.0.0/16)"
	ErrDetailVpcCidrPrefixBand = "prefix must be between /16 and /24"
	ErrDetailVpcCidrNotRfc1918 = "must lie inside an RFC1918 private range (10/8, 172.16/12, 192.168/16)"
)

// A failed VPC still exists, and destroy is its only exit.
const (
	WarnSummaryVpcFailed        = "VPC is in the failed state"
	WarnDetailVpcFailed         = "VPC %s is in the failed state: %s. Destroy the VPC and create it again."
	WarnDetailVpcFailedNoReason = "the platform reported no reason"
)

const noChildren = "none"

// CreateFailureDiagnostic names the confirm gate for what it is. The overlap
// 409 asks a question, so the answer belongs to the operator and never to the
// provider.
func CreateFailureDiagnostic(err error) diag.Diagnostic {
	overlapping, isOverlap := overlappingVpcs(err)
	if !isOverlap {
		return diag.NewErrorDiagnostic(ErrSummaryUnableToCreateVpc, err.Error())
	}
	if overlapping == "" {
		return diag.NewErrorDiagnostic(
			ErrSummaryVpcCidrOverlap,
			fmt.Sprintf(ErrDetailVpcCidrOverlapHidden, err.Error()),
		)
	}
	return diag.NewErrorDiagnostic(
		ErrSummaryVpcCidrOverlap,
		fmt.Sprintf(ErrDetailVpcCidrOverlap, err.Error(), overlapping),
	)
}

// DeleteFailureDiagnostic renders the census the API refused on. The sentence
// alone totals the two populations, and only the split tells the reader which
// rows they can delete now.
func DeleteFailureDiagnostic(vpcID string, err error) diag.Diagnostic {
	details, isNotEmpty := childCensus(err)
	if !isNotEmpty {
		return diag.NewErrorDiagnostic(
			ErrSummaryUnableToDeleteVpc,
			fmt.Sprintf(ErrDetailUnableToDeleteVpcWithID, vpcID)+": "+err.Error(),
		)
	}
	return diag.NewErrorDiagnostic(
		ErrSummaryUnableToDeleteVpc,
		fmt.Sprintf(ErrDetailVpcNotEmpty, err.Error(), details.blockers, details.inFlight),
	)
}

func FailedVpcWarning(vpcID, failureReason string) diag.Diagnostic {
	if failureReason == "" {
		failureReason = WarnDetailVpcFailedNoReason
	}
	return diag.NewWarningDiagnostic(
		WarnSummaryVpcFailed,
		fmt.Sprintf(WarnDetailVpcFailed, vpcID, failureReason),
	)
}

type vpcCensus struct {
	blockers string
	inFlight string
}

func childCensus(err error) (vpcCensus, bool) {
	details, ok := errorDetails(err, ERROR_CODE_VPC_NOT_EMPTY)
	if !ok {
		return vpcCensus{}, false
	}
	return vpcCensus{
		blockers: renderChildCounts(details["blockers"]),
		inFlight: renderChildCounts(details["inFlight"]),
	}, true
}

// The kinds keep the API's own nouns, so the rendered list and the sentence
// above it name the same things.
func renderChildCounts(counts any) string {
	byKind, ok := counts.(map[string]any)
	if !ok {
		return noChildren
	}
	kinds := []struct {
		key  string
		noun string
	}{
		{key: "subnets", noun: "subnet(s)"},
		{key: "publicIps", noun: "public IP(s)"},
		{key: "nsgs", noun: "network security group(s)"},
	}
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		count, isNumber := byKind[kind.key].(float64)
		if !isNumber || count <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", int(count), kind.noun))
	}
	if len(parts) == 0 {
		return noChildren
	}
	return strings.Join(parts, ", ")
}

func overlappingVpcs(err error) (string, bool) {
	details, ok := errorDetails(err, ERROR_CODE_CIDR_OVERLAP_UNCONFIRMED)
	if !ok {
		return "", false
	}
	rows, ok := details["overlapping"].([]any)
	if !ok {
		return "", true
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		fields, isObject := row.(map[string]any)
		if !isObject {
			continue
		}
		name, hasName := fields["name"].(string)
		cidr, hasCidr := fields["cidr"].(string)
		if !hasName || !hasCidr {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, cidr))
	}
	return strings.Join(parts, ", "), true
}

func errorDetails(err error, code string) (map[string]any, bool) {
	if !client.HasErrorCode(err, code) {
		return nil, false
	}
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return nil, false
	}
	return httpErr.Details, true
}
