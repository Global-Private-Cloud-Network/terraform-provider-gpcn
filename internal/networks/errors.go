package networks

// Error summary constants
const (
	ErrSummaryMissingRequiredAttr     = "Missing required attribute"
	ErrSummaryInvalidAttr             = "Attribute is invalid"
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateNetwork   = "Unable to create GPCN Network"
	ErrSummaryUnableToGetNetwork      = "Unable to get GPCN Network"
	ErrSummaryUnableToUpdateNetwork   = "Unable to update GPCN Network"
	ErrSummaryUnableToDeleteNetwork   = "Unable to delete GPCN Network"
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
)
