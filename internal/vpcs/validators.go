package vpcs

import (
	"context"
	"regexp"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const (
	superCidrMinPrefix = 16
	superCidrMaxPrefix = 24
)

var ipv4CidrPattern = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})/(\d{1,2})$`)

type cidrRange struct {
	start  uint32
	end    uint32
	prefix int
}

// The RFC1918 blocks GPCN accepts, as ranges.
var rfc1918Blocks = []cidrRange{
	{start: 0x0A000000, end: 0x0AFFFFFF},
	{start: 0xAC100000, end: 0xAC1FFFFF},
	{start: 0xC0A80000, end: 0xC0A8FFFF},
}

// SuperCidrValidator applies the super-CIDR rules GPCN applies, in the order
// GPCN applies them. A plan that disagrees with the API teaches the rule one
// failed apply at a time.
type SuperCidrValidator struct{}

var _ validator.String = SuperCidrValidator{}

func (v SuperCidrValidator) Description(_ context.Context) string {
	return "CIDR must be an IPv4 CIDR at its network address, with a prefix between /16 and /24, inside an RFC1918 private range"
}

func (v SuperCidrValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v SuperCidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	parsed, ok := parseCidr(req.ConfigValue.ValueString())
	if !ok {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidVpcCidr, ErrDetailVpcCidrShape)
		return
	}
	if parsed.prefix < superCidrMinPrefix || parsed.prefix > superCidrMaxPrefix {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidVpcCidr, ErrDetailVpcCidrPrefixBand)
		return
	}
	if !isRfc1918(parsed) {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidVpcCidr, ErrDetailVpcCidrNotRfc1918)
	}
}

// parseCidr mirrors the backend's CIDR parser.
// The backend refuses a leading-zero octet, because an inet_aton parser reads
// such an octet as octal.
func parseCidr(cidr string) (cidrRange, bool) {
	match := ipv4CidrPattern.FindStringSubmatch(cidr)
	if match == nil {
		return cidrRange{}, false
	}
	var address uint32
	for _, field := range match[1:5] {
		if hasLeadingZero(field) {
			return cidrRange{}, false
		}
		octet, err := strconv.Atoi(field)
		if err != nil || octet > 255 {
			return cidrRange{}, false
		}
		//nolint:gosec // G115: the octet is refused above unless it is 0 to 255.
		address = address<<8 | uint32(octet)
	}
	if hasLeadingZero(match[5]) {
		return cidrRange{}, false
	}
	prefix, err := strconv.Atoi(match[5])
	if err != nil || prefix > 32 {
		return cidrRange{}, false
	}

	var mask uint32
	if prefix > 0 {
		mask = ^uint32(0) << (32 - prefix)
	}
	start := address & mask
	if start != address {
		return cidrRange{}, false
	}
	return cidrRange{start: start, end: start | ^mask, prefix: prefix}, true
}

func hasLeadingZero(field string) bool {
	return len(field) > 1 && field[0] == '0'
}

func isRfc1918(subject cidrRange) bool {
	for _, block := range rfc1918Blocks {
		if subject.start >= block.start && subject.end <= block.end {
			return true
		}
	}
	return false
}
