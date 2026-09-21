package networks

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Error summary constants
const (
	ErrSummaryMissingRequiredAttr     = "Missing required attribute"
	ErrSummaryInvalidAttr             = "Attribute is invalid"
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateNetwork   = "Unable to create GPCN Network"
	ErrSummaryUnableToGetNetwork      = "Unable to get GPCN Network"
	ErrSummaryUnableToUpdateNetwork   = "Unable to update GPCN Network"
	ErrSummaryUnableToDeleteNetwork   = "Unable to delete GPCN Network"
	ErrSummaryNetworkCreateRetired    = "Network creation is no longer supported"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient           = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailAttrRequiredForStandard      = "Attribute '%s' must be set when 'network_type' is 'standard'."
	ErrDetailNotValidIPv4                 = "The attribute '%s' does not resolve to a valid IPv4 address"
	ErrDetailNotValidIPv4WithValue        = "The attribute '%s' does not resolve to a valid IPv4 address. The value '%s' is not a valid IPv4 address"
	ErrDetailNotInCIDRBlock               = "The attribute '%s' is not a valid IP address in the CIDR block"
	ErrDetailNotValidCIDRBlock            = "The attribute '%s' does not contain a valid CIDR block"
	ErrDetailCIDRBlockNotNetworkAddr      = "The attribute '%s' does not contain a valid CIDR block. The IP address is not the network address for the given mask"
	ErrDetailCIDRBlockInvalidIP           = "The attribute '%s' does not contain a CIDR block with a valid IP address"
	ErrDetailRemoveNetworkInterfaceFailed = "failed to detach network interface for ID '%s' before deleting. Unable to delete a network still attached to a virtual machine"
	ErrDetailUnableToGetNetworkWithID     = "Unable to get GPCN Network with ID '%s'"
	ErrDetailUnableToUpdateNetworkWithID  = "Unable to update GPCN Network with ID '%s'"
	ErrDetailUnableToDeleteNetworkWithID  = "Unable to delete GPCN Network with ID '%s'"

	ErrDetailNetworkCreateRetired = "GPCN has moved to VPC networking. Create a gpcn_vpc and a gpcn_vpc_subnet for routed networking, or a gpcn_l2_segment for a layer-2 network. Existing gpcn_network resources can still be read and destroyed."

	ErrDetailNoCandidateNetworkInterface    = "the virtual machine has no candidate network interface to promote"
	ErrDetailReplacePrimaryInterfaceFailed  = "error replacing primary interface: %w"
	ErrDetailRefreshNetworkInterfacesFailed = "error refreshing the network interfaces of virtual machine ID '%s' before promoting a primary: %w"
)

// Warning strings for a custom network the platform adopted into an L2 segment
const (
	WarnSummaryNetworkRemovedFromState = "Network removed from state"
	WarnDetailCustomNetworkGone        = "Network %s was not found. If it was adopted into an L2 segment by the platform, remove it from state and import the segment as gpcn_l2_segment: terraform state rm %s && terraform import gpcn_l2_segment.<name> <segment-id>."
)

// The platform answers the same 404 for an adopted network as for a typo. The message
// therefore names both readings. It also names the state move that recovers the segment.
func CustomNetworkGoneWarning(networkID string) diag.Diagnostic {
	return diag.NewWarningDiagnostic(
		WarnSummaryNetworkRemovedFromState,
		fmt.Sprintf(WarnDetailCustomNetworkGone, networkID, networkID),
	)
}
