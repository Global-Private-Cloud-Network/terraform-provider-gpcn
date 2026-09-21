package vpcnsgs

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// NoOuterWhitespaceValidator refuses a value GPCN stores in a trimmed form. The
// stored value then differs from the configuration, so the plan never settles.
// The provider does not trim on the operator's behalf. The stored name would
// no longer be the one the configuration names.
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
		ErrSummaryInvalidNsgAttribute,
		fmt.Sprintf(ErrDetailOuterWhitespace, v.Attribute),
	)
}

// RulePortValidator judges a rule's port pair. The rule needs all of its own
// attributes to do it. It therefore validates the block rather than one
// attribute. Every refusal quotes the sentence GPCN answers with. The plan and
// the apply then refuse a rule in the same words.
type RulePortValidator struct{}

func (v RulePortValidator) Description(_ context.Context) string {
	return "Ports apply to the tcp and udp protocols only, must be given as a pair, and the minimum must not exceed the maximum."
}

func (v RulePortValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v RulePortValidator) ValidateObject(ctx context.Context, request validator.ObjectRequest, response *validator.ObjectResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}

	var rule RuleModel
	diags := request.ConfigValue.As(ctx, &rule, basetypes.ObjectAsOptions{})
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	// A value from a variable is unknown at plan time. GPCN judges it on the
	// request, which is the only place it is known.
	if rule.Protocol.IsUnknown() || rule.PortRangeMin.IsUnknown() || rule.PortRangeMax.IsUnknown() {
		return
	}

	protocol := rule.Protocol.ValueString()
	portable := protocol == "tcp" || protocol == "udp"
	hasMin := !rule.PortRangeMin.IsNull()
	hasMax := !rule.PortRangeMax.IsNull()

	if !portable && (hasMin || hasMax) {
		response.Diagnostics.AddAttributeError(
			request.Path,
			ErrSummaryInvalidRule,
			fmt.Sprintf(ErrDetailRulePortsNotApplicable, protocol),
		)
		return
	}
	if hasMin != hasMax {
		response.Diagnostics.AddAttributeError(request.Path, ErrSummaryInvalidRule, ErrDetailRulePortsTogether)
		return
	}
	if hasMin && hasMax && rule.PortRangeMin.ValueInt64() > rule.PortRangeMax.ValueInt64() {
		response.Diagnostics.AddAttributeError(request.Path, ErrSummaryInvalidRule, ErrDetailRulePortOrder)
	}
}
