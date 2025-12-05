package networks

// Error summary constants
const (
	ErrSummaryMissingRequiredAttr = "Missing required attribute"
	ErrSummaryInvalidAttr         = "Attribute is invalid"
)

// Error detail message templates
const (
	ErrDetailAttrRequiredForStandard      = "Attribute '%s' must be set when 'network_type' is 'standard'."
	ErrDetailNotValidIPv4                 = "The attribute '%s' does not resolve to a valid IPv4 address"
	ErrDetailNotValidIPv4WithValue        = "The attribute '%s' does not resolve to a valid IPv4 address. The value '%s' is not a valid IPv4 address"
	ErrDetailNotInCIDRBlock               = "The attribute '%s' is not a valid IP address in the CIDR block"
	ErrDetailNotValidCIDRBlock            = "The attribute '%s' does not contain a valid CIDR block"
	ErrDetailCIDRBlockNotNetworkAddr      = "The attribute '%s' does not contain a valid CIDR block. The IP address is not the network address for the given mask"
	ErrDetailCIDRBlockInvalidIP           = "The attribute '%s' does not contain a CIDR block with a valid IP address"
	ErrDetailDNSInvalidDelimiter          = "The attribute '%s' must use comma-space (', ') as the delimiter between DNS server addresses. Example: '8.8.8.8, 8.8.4.4'"
	ErrDetailDNSSpaceBeforeComma          = "The attribute '%s' must use comma-space (', ') as the delimiter. Space before comma is not allowed. Example: '8.8.8.8, 8.8.4.4'"
	ErrDetailRemoveNetworkInterfaceFailed = "failed to detach network interface for ID: %s before deleting. Unable to delete a network still attached to a virtual machine"
)
