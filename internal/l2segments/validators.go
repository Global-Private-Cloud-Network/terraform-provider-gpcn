package l2segments

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NoOuterWhitespaceValidator refuses a value GPCN stores in a trimmed form.
// Attribute names the schema attribute the message points at.
type NoOuterWhitespaceValidator struct {
	Attribute string
}

var _ validator.String = NoOuterWhitespaceValidator{}

func (v NoOuterWhitespaceValidator) Description(_ context.Context) string {
	return fmt.Sprintf("%s must not start or end with whitespace", v.Attribute)
}

func (v NoOuterWhitespaceValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v NoOuterWhitespaceValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	// GPCN trims the value before it validates and stores it. A padded value
	// comes back different and makes the plan never settle.
	value := req.ConfigValue.ValueString()
	if strings.TrimSpace(value) == value {
		return
	}

	resp.Diagnostics.AddAttributeError(
		req.Path,
		fmt.Sprintf(ErrSummaryInvalidL2SegmentAttribute, v.Attribute),
		fmt.Sprintf(ErrDetailL2SegmentOuterWhitespace, v.Attribute),
	)
}
