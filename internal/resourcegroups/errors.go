package resourcegroups

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"terraform-provider-gpcn/internal/client"
)

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType     = "Unexpected Configure Type"
	ErrSummaryUnableToCreateResourceGroup = "Unable to Create Resource Group"
	ErrSummaryUnableToReadResourceGroup   = "Unable to Read Resource Group"
	ErrSummaryUnableToUpdateResourceGroup = "Unable to Update Resource Group"
	ErrSummaryUnableToDeleteResourceGroup = "Unable to Delete Resource Group"
	ErrSummaryUnableToFetchResourceGroups = "Unable to Fetch Resource Groups"
	ErrSummaryNoResourceGroupMatches      = "No Resource Group Matches"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient        = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadResourceGroupFailed   = "Failed to read resource group with ID %s"
	ErrDetailUpdateResourceGroupFailed = "Failed to update resource group with ID %s"
	ErrDetailDeleteResourceGroupFailed = "Failed to delete resource group with ID %s"
	ErrDetailNoResourceGroupMatches    = "No resource group named %q is visible to this API key."
	ErrDetailResourceGroupListTooLong  = "The resource group list has %d pages, and the provider reads at most %d"

	ErrDetailResourceGroupIsDefault = "Resource group %s is the tenant's Default resource group, and GPCN does not delete it. To stop managing it with Terraform, run: terraform state rm <address>"
	ErrDetailResourceGroupNotEmpty  = "Resource group %s still has resources assigned to it%s. Move them to another resource group, then destroy it again."
)

// DeleteRefusalDetail renders the two refusals the user can act on. The second
// result is false for any other error, which the caller reports unchanged.
func DeleteRefusalDetail(groupID string, err error) (string, bool) {
	switch {
	case client.HasErrorCode(err, ErrorCodeResourceGroupIsDefault):
		return fmt.Sprintf(ErrDetailResourceGroupIsDefault, groupID), true
	case client.HasErrorCode(err, ErrorCodeResourceGroupNotEmpty):
		return fmt.Sprintf(ErrDetailResourceGroupNotEmpty, groupID, renderLiveCounts(err)), true
	default:
		return "", false
	}
}

// renderLiveCounts returns the parenthesised counts with their leading space,
// or an empty string when the API sent no non-zero count.
func renderLiveCounts(err error) string {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return ""
	}
	parts := make([]string, 0, len(httpErr.Details))
	for key, raw := range httpErr.Details {
		count, isNumber := numericCount(raw)
		if !isNumber || count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %d", key, count))
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return " (" + strings.Join(parts, ", ") + ")"
}

// A client that pre-decodes numbers can hand over a json.Number instead of a
// float64.
func numericCount(raw any) (int64, bool) {
	switch value := raw.(type) {
	case float64:
		return int64(value), true
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
