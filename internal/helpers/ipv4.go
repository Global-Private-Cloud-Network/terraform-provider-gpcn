package helpers

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Messages for the IPv4 validators. The attribute name fills the first %s.
const (
	ErrSummaryInvalidIPv4Address = "Invalid IPv4 address"
	ErrSummaryInvalidIPv4CIDR    = "Invalid IPv4 CIDR"

	ErrDetailIPv4Format       = "%s must be an IPv4 address in dotted-quad form, such as 203.0.113.10. Got: '%s'"
	ErrDetailIPv4LeadingZero  = "%s must not have leading zeros in an octet. Got: '%s'"
	ErrDetailIPv4NotPublic    = "%s must be a public IPv4 address. %s is in the non-public range %s"
	ErrDetailCIDRFormat       = "%s must be an IPv4 CIDR, such as 192.168.10.0/24. Got: '%s'"
	ErrDetailCIDRLeadingZero  = "%s must not have leading zeros in an octet or in the prefix length. Got: '%s'"
	ErrDetailCIDRPrefixBounds = "%s must have a prefix length between /%d and /32. Got: '%s'"
	ErrDetailCIDRHostBitsFmt  = "%s must be written as its network address. '%s' has host bits set. Use '%s'"
	descIPv4Address           = "%s must be an IPv4 address in dotted-quad form"
	descIPv4AddressPublicOnly = "%s must be a public IPv4 address in dotted-quad form"
	descIPv4CIDRFmt           = "%s must be an IPv4 CIDR with a prefix length from /%d to /32, written as its network address"
	maxIPv4Prefix             = 32
	ipv4OctetCount            = 4
	ipv4MaxOctetLen           = 3
	cidrPartCount             = 2
	ipv4OctetMax              = 255
	ipv4OctetSeparator        = "."
	cidrPrefixSeparator       = "/"
)

// reservedIPv4Ranges are the ranges a peer gateway address cannot be in. The
// API refuses them.
var reservedIPv4Ranges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// parseDottedQuad parses an IPv4 address and tells a leading zero apart from
// any other format fault. The standard parser refuses both, but with one message.
func parseDottedQuad(value string) (addr netip.Addr, leadingZero, ok bool) {
	parts := strings.Split(value, ipv4OctetSeparator)
	if len(parts) != ipv4OctetCount {
		return netip.Addr{}, false, false
	}
	for _, part := range parts {
		if part == "" || len(part) > ipv4MaxOctetLen {
			return netip.Addr{}, false, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || number > ipv4OctetMax || part[0] == '+' || part[0] == '-' {
			return netip.Addr{}, false, false
		}
		if len(part) > 1 && part[0] == '0' {
			leadingZero = true
		}
	}
	if leadingZero {
		return netip.Addr{}, true, false
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return netip.Addr{}, false, false
	}
	return addr, false, true
}

// ValidateIPv4Address returns the reason value is not an IPv4 address, or an
// empty string. With publicOnly set, a reserved range is also refused.
func ValidateIPv4Address(attribute, value string, publicOnly bool) string {
	addr, leadingZero, ok := parseDottedQuad(value)
	if leadingZero {
		return fmt.Sprintf(ErrDetailIPv4LeadingZero, attribute, value)
	}
	if !ok {
		return fmt.Sprintf(ErrDetailIPv4Format, attribute, value)
	}
	if publicOnly {
		for _, reserved := range reservedIPv4Ranges {
			if reserved.Contains(addr) {
				return fmt.Sprintf(ErrDetailIPv4NotPublic, attribute, value, reserved)
			}
		}
	}
	return ""
}

// ValidateIPv4CIDR returns the reason value is not an IPv4 CIDR in network
// form with a prefix of at least minPrefix, or an empty string.
func ValidateIPv4CIDR(attribute, value string, minPrefix int) string {
	parts := strings.Split(value, cidrPrefixSeparator)
	if len(parts) != cidrPartCount {
		return fmt.Sprintf(ErrDetailCIDRFormat, attribute, value)
	}
	addr, leadingZero, ok := parseDottedQuad(parts[0])
	if leadingZero {
		return fmt.Sprintf(ErrDetailCIDRLeadingZero, attribute, value)
	}
	if !ok {
		return fmt.Sprintf(ErrDetailCIDRFormat, attribute, value)
	}

	prefixText := parts[1]
	if prefixText == "" || len(prefixText) > ipv4MaxOctetLen || strings.TrimLeft(prefixText, "0123456789") != "" {
		return fmt.Sprintf(ErrDetailCIDRFormat, attribute, value)
	}
	if len(prefixText) > 1 && prefixText[0] == '0' {
		return fmt.Sprintf(ErrDetailCIDRLeadingZero, attribute, value)
	}
	bits, err := strconv.Atoi(prefixText)
	if err != nil {
		return fmt.Sprintf(ErrDetailCIDRFormat, attribute, value)
	}
	if bits < minPrefix || bits > maxIPv4Prefix {
		return fmt.Sprintf(ErrDetailCIDRPrefixBounds, attribute, minPrefix, value)
	}

	network := netip.PrefixFrom(addr, bits).Masked()
	if network.Addr() != addr {
		return fmt.Sprintf(ErrDetailCIDRHostBitsFmt, attribute, value, network)
	}
	return ""
}

// IPv4AddressValidator refuses a string that is not an IPv4 address. With
// PublicOnly set, it also refuses the reserved ranges.
type IPv4AddressValidator struct {
	// Attribute names the schema attribute the message points at.
	Attribute  string
	PublicOnly bool
}

var _ validator.String = IPv4AddressValidator{}

func (v IPv4AddressValidator) Description(_ context.Context) string {
	if v.PublicOnly {
		return fmt.Sprintf(descIPv4AddressPublicOnly, v.Attribute)
	}
	return fmt.Sprintf(descIPv4Address, v.Attribute)
}

func (v IPv4AddressValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v IPv4AddressValidator) ValidateString(_ context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}
	if reason := ValidateIPv4Address(v.Attribute, request.ConfigValue.ValueString(), v.PublicOnly); reason != "" {
		response.Diagnostics.AddAttributeError(request.Path, ErrSummaryInvalidIPv4Address, reason)
	}
}

// IPv4CIDRValidator refuses a string that is not an IPv4 CIDR written as its
// network address, with a prefix from MinPrefix to 32.
type IPv4CIDRValidator struct {
	// Attribute names the schema attribute the message points at.
	Attribute string
	MinPrefix int
}

var _ validator.String = IPv4CIDRValidator{}

func (v IPv4CIDRValidator) Description(_ context.Context) string {
	return fmt.Sprintf(descIPv4CIDRFmt, v.Attribute, v.MinPrefix)
}

func (v IPv4CIDRValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v IPv4CIDRValidator) ValidateString(_ context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}
	if reason := ValidateIPv4CIDR(v.Attribute, request.ConfigValue.ValueString(), v.MinPrefix); reason != "" {
		response.Diagnostics.AddAttributeError(request.Path, ErrSummaryInvalidIPv4CIDR, reason)
	}
}
