package vpcsubnets

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NoOuterWhitespaceValidator refuses a value GPCN stores in a trimmed form. The
// stored value then differs from the configuration, so the plan never settles.
// The provider does not trim on the operator's behalf, because the stored name
// would no longer be the one the configuration names.
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

func (v NoOuterWhitespaceValidator) ValidateString(_ context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}

	value := request.ConfigValue.ValueString()
	if strings.TrimSpace(value) == value {
		return
	}

	response.Diagnostics.AddAttributeError(
		request.Path,
		ErrSummaryInvalidSubnetAttribute,
		fmt.Sprintf(ErrDetailOuterWhitespace, v.Attribute),
	)
}
