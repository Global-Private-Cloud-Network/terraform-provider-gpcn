package networks

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// StandardNetworkValidator validates that required attributes are present when
// network_type is "standard".
//
// Accumulates all errors to show user all missing required attributes at once,
// since each check is independent.
type StandardNetworkValidator struct{}

func (v StandardNetworkValidator) Description(ctx context.Context) string {
	return "Ensures attributes 'cidr_block', 'dhcp_start_address', 'dhcp_end_address', and 'dns_servers' are present when attribute 'network_type' is set to 'standard'."
}
func (v StandardNetworkValidator) MarkdownDescription(ctx context.Context) string {
	return "Ensures attributes 'cidr_block', 'dhcp_start_address', 'dhcp_end_address', and 'dns_servers' are present when attribute 'network_type' is set to 'standard'."
}
func (v StandardNetworkValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	// Access the full configuration to check other attributes
	var config ResourceModel
	diags := request.Config.Get(ctx, &config)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	if config.NetworkType.ValueString() == "standard" && config.CIDRBlock.IsNull() {
		response.Diagnostics.AddError(
			ErrSummaryMissingRequiredAttr,
			fmt.Sprintf(ErrDetailAttrRequiredForStandard, "cidr_block"),
		)
	}
	if config.NetworkType.ValueString() == "standard" && config.DHCPStartAddress.IsNull() {
		response.Diagnostics.AddError(
			ErrSummaryMissingRequiredAttr,
			fmt.Sprintf(ErrDetailAttrRequiredForStandard, "dhcp_start_address"),
		)
	}
	if config.NetworkType.ValueString() == "standard" && config.DHCPEndAddress.IsNull() {
		response.Diagnostics.AddError(
			ErrSummaryMissingRequiredAttr,
			fmt.Sprintf(ErrDetailAttrRequiredForStandard, "dhcp_end_address"),
		)
	}
	if config.NetworkType.ValueString() == "standard" && config.DNSServers.IsNull() {
		response.Diagnostics.AddError(
			ErrSummaryMissingRequiredAttr,
			fmt.Sprintf(ErrDetailAttrRequiredForStandard, "dns_servers"),
		)
	}
}

// IpAddressValidator validates that a string attribute is a valid IPv4 address
// and optionally validates it's within the configured CIDR block.
//
// Returns early on first error since subsequent validations depend on earlier ones:
// - If the value isn't a valid IP, checking IPv4 compatibility is meaningless
// - If it's not IPv4, checking CIDR containment is meaningless
type IpAddressValidator struct{}

func (v IpAddressValidator) Description(ctx context.Context) string {
	return "Ensures attribute resolves to a valid IPv4 address"
}
func (v IpAddressValidator) MarkdownDescription(ctx context.Context) string {
	return "Ensures attribute resolves to a valid IPv4 address"
}
func (v IpAddressValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	// Access the full configuration to check other attributes
	var config ResourceModel
	diags := request.Config.Get(ctx, &config)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	// Check for optional case
	if request.ConfigValue.ValueString() == "" {
		return
	}

	parsedIPAddress := net.ParseIP(request.ConfigValue.ValueString())
	if parsedIPAddress == nil {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailNotValidIPv4, request.Path.Expression().String()),
		)
		return
	}
	parsedIPAddressv4 := parsedIPAddress.To4()
	if parsedIPAddressv4 == nil {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailNotValidIPv4, request.Path.Expression().String()),
		)
		return
	}

	// Verify the attribute is a valid part of the CIDR block
	if config.CIDRBlock.IsNull() {
		return
	}

	_, parsedIpNet, err := net.ParseCIDR(config.CIDRBlock.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailNotInCIDRBlock, request.Path.Expression().String()),
		)
		return
	}

	if !parsedIpNet.Contains(parsedIPAddress) {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailNotInCIDRBlock, request.Path.Expression().String()),
		)
		return
	}
}

// CIDRValidator validates that a string attribute is a valid CIDR block.
//
// Returns early on first error since subsequent validations depend on earlier ones:
// - If the value isn't valid CIDR syntax, checking network address is meaningless
// - If it's not the network address, checking IPv4 compatibility is meaningless
type CIDRValidator struct{}

func (v CIDRValidator) Description(ctx context.Context) string {
	return "Ensures attribute resolves to a valid CIDR address"
}
func (v CIDRValidator) MarkdownDescription(ctx context.Context) string {
	return "Ensures attribute resolves to a valid CIDR address"
}
func (v CIDRValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	// Check for optional case
	if request.ConfigValue.ValueString() == "" {
		return
	}

	parsedIP, parsedIpNet, err := net.ParseCIDR(request.ConfigValue.ValueString())
	if err != nil {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailNotValidCIDRBlock, request.Path.Expression().String()),
		)
		return
	}

	if !parsedIP.Equal(parsedIpNet.IP) {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailCIDRBlockNotNetworkAddr, request.Path.Expression().String()),
		)
		return
	}

	parsedIPv4 := parsedIP.To4()
	if parsedIPv4 == nil {
		response.Diagnostics.AddError(
			ErrSummaryInvalidAttr,
			fmt.Sprintf(ErrDetailCIDRBlockInvalidIP, request.Path.Expression().String()),
		)
		return
	}
}

// DNSServersValidator validates that a comma-delimited string contains valid IPv4 addresses.
//
// Error handling pattern:
// - Returns early on delimiter format errors (can't reliably parse with wrong delimiter)
// - Accumulates IP validation errors to show all invalid addresses at once
type DNSServersValidator struct{}

func (v DNSServersValidator) Description(ctx context.Context) string {
	return "Parses a comma-delimited string and verifies each IP address is a valid one"
}
func (v DNSServersValidator) MarkdownDescription(ctx context.Context) string {
	return "Parses a comma-delimited string and verifies each IP address is a valid one"
}
func (v DNSServersValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	// Access the full configuration to check other attributes
	var config ResourceModel
	diags := request.Config.Get(ctx, &config)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	// Check for optional case
	if request.ConfigValue.ValueString() == "" {
		return
	}

	value := request.ConfigValue.ValueString()

	// Validate delimiter format first - if invalid, we can't reliably parse individual IPs
	if strings.Contains(value, ",") {
		hasValidDelimiter := strings.Contains(value, ", ")
		hasSpaceBeforeComma := strings.Contains(value, " ,")

		if !hasValidDelimiter {
			response.Diagnostics.AddError(
				ErrSummaryInvalidAttr,
				fmt.Sprintf(ErrDetailDNSInvalidDelimiter, request.Path.Expression().String()),
			)
			// Return early - can't reliably parse IPs with invalid delimiter
			return
		}

		if hasSpaceBeforeComma {
			response.Diagnostics.AddError(
				ErrSummaryInvalidAttr,
				fmt.Sprintf(ErrDetailDNSSpaceBeforeComma, request.Path.Expression().String()),
			)
			// Return early - can't reliably parse IPs with invalid delimiter
			return
		}
	}

	// Validate each IP address - accumulate all errors to show user all invalid IPs at once
	ipAddresses := strings.SplitSeq(value, ", ")
	for address := range ipAddresses {
		addr := strings.TrimSpace(address)
		parsedIPAddress := net.ParseIP(addr)
		if parsedIPAddress == nil {
			response.Diagnostics.AddError(
				ErrSummaryInvalidAttr,
				fmt.Sprintf(ErrDetailNotValidIPv4WithValue, request.Path.Expression().String(), addr),
			)
			continue
		}
		parsedIPAddressv4 := parsedIPAddress.To4()
		if parsedIPAddressv4 == nil {
			response.Diagnostics.AddError(
				ErrSummaryInvalidAttr,
				fmt.Sprintf(ErrDetailNotValidIPv4WithValue, request.Path.Expression().String(), addr),
			)
			continue
		}
	}
}
