package vpcnsgs

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// RulePortValidator judges a rule's port pair. The rule needs all of its own
// attributes to do it, which is why it validates the block rather than one
// attribute. Every refusal quotes the sentence GPCN answers with, so the plan
// and the apply refuse a rule in the same words.
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
