package vpcnsgs

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID            types.String `tfsdk:"id"`
	VpcID         types.String `tfsdk:"vpc_id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Rules         types.Set    `tfsdk:"rule"`
	IsDefault     types.Bool   `tfsdk:"is_default"`
	State         types.String `tfsdk:"state"`
	FailureReason types.String `tfsdk:"failure_reason"`
	RuleCount     types.Int64  `tfsdk:"rule_count"`
	SubnetCount   types.Int64  `tfsdk:"subnet_count"`
	CreatedTime   types.String `tfsdk:"created_time"`
	LastUpdated   types.String `tfsdk:"last_updated"`
}

// RuleModel is one `rule` block. The API's rule ID is absent on purpose: it is
// ignored on write, and a set keyed on it would churn on every replace.
type RuleModel struct {
	Direction    types.String `tfsdk:"direction"`
	Protocol     types.String `tfsdk:"protocol"`
	PortRangeMin types.Int64  `tfsdk:"port_range_min"`
	PortRangeMax types.Int64  `tfsdk:"port_range_max"`
	RemoteCidr   types.String `tfsdk:"remote_cidr"`
	Description  types.String `tfsdk:"description"`
}

// RuleObjectType names the element type of the rule set. The set conversions
// and the object validator both need it.
func RuleObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"direction":      types.StringType,
		"protocol":       types.StringType,
		"port_range_min": types.Int64Type,
		"port_range_max": types.Int64Type,
		"remote_cidr":    types.StringType,
		"description":    types.StringType,
	}}
}

// formatTimestamp renders an API timestamp in the house format. A timestamp the
// API sends in another shape reads as "unknown" rather than failing the apply.
func formatTimestamp(raw string) types.String {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return types.StringValue("unknown")
	}
	return types.StringValue(parsed.Format(time.RFC850))
}

// optionalString maps a nullable API field. A null stays null, because a
// Computed attribute that shows "" for an absent value hides the difference.
func optionalString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

func optionalInt64(raw *int64) types.Int64 {
	if raw == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*raw)
}

// isUnset reports whether the model has no value the caller chose. An import
// leaves a null, and a Computed attribute reaches Create as unknown.
func isUnset(value types.String) bool {
	return value.IsNull() || value.IsUnknown()
}

// DefaultNsgRulesWarning reports what a replace costs on the VPC's own default
// group. GPCN stages the platform posture there as ordinary rule rows and puts
// no guard on the rules route, so the configured set silently replaces them.
func DefaultNsgRulesWarning(isDefault bool, nsgID string) diag.Diagnostics {
	var diags diag.Diagnostics
	if isDefault {
		diags.AddWarning(WarnSummaryDefaultNsgRulesReplaced, fmt.Sprintf(WarnDetailDefaultNsgRulesReplaced, nsgID))
	}
	return diags
}

// RulesToSet converts the API's rules into the set the schema declares.
func RulesToSet(ctx context.Context, rules []ApiRule) (types.Set, diag.Diagnostics) {
	models := make([]RuleModel, 0, len(rules))
	for _, rule := range rules {
		models = append(models, RuleModel{
			Direction:    types.StringValue(rule.Direction),
			Protocol:     types.StringValue(rule.Protocol),
			PortRangeMin: optionalInt64(rule.PortRangeMin),
			PortRangeMax: optionalInt64(rule.PortRangeMax),
			RemoteCidr:   types.StringValue(rule.RemoteCidr),
			Description:  optionalString(rule.Description),
		})
	}
	return types.SetValueFrom(ctx, RuleObjectType(), models)
}

// RulesFromSet reads the rule blocks out of a plan or a state.
func RulesFromSet(ctx context.Context, set types.Set) ([]RuleModel, diag.Diagnostics) {
	rules := []RuleModel{}
	if set.IsNull() || set.IsUnknown() {
		return rules, nil
	}
	diags := set.ElementsAs(ctx, &rules, false)
	return rules, diags
}

// RuleRequestBodies builds the payload the create and the replace both send. A
// null port or description is left out: the API reads an absent key and a null
// one the same way, and the body is strict about the keys it does not know.
func RuleRequestBodies(rules []RuleModel) []map[string]any {
	bodies := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		body := map[string]any{
			"direction":  rule.Direction.ValueString(),
			"protocol":   rule.Protocol.ValueString(),
			"remoteCidr": rule.RemoteCidr.ValueString(),
		}
		if !rule.PortRangeMin.IsNull() && !rule.PortRangeMin.IsUnknown() {
			body["portRangeMin"] = rule.PortRangeMin.ValueInt64()
		}
		if !rule.PortRangeMax.IsNull() && !rule.PortRangeMax.IsUnknown() {
			body["portRangeMax"] = rule.PortRangeMax.ValueInt64()
		}
		if !rule.Description.IsNull() && !rule.Description.IsUnknown() {
			body["description"] = rule.Description.ValueString()
		}
		bodies = append(bodies, body)
	}
	return bodies
}

// MapNsgResponseToModel writes the Computed attributes and fills the
// configurable ones only when the caller chose no value, which is what an
// import and a Create both leave behind.
func MapNsgResponseToModel(ctx context.Context, response *NsgDetail, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	model.ID = types.StringValue(response.Nsg.ID)
	model.IsDefault = types.BoolValue(response.Nsg.IsDefault)
	model.State = types.StringValue(response.Nsg.State)
	model.FailureReason = optionalString(response.Nsg.FailureReason)
	model.RuleCount = types.Int64Value(response.Nsg.RuleCount)
	model.SubnetCount = types.Int64Value(response.Nsg.SubnetCount)
	model.CreatedTime = formatTimestamp(response.Nsg.CreatedAt)
	model.LastUpdated = formatTimestamp(response.Nsg.UpdatedAt)

	if isUnset(model.Name) {
		model.Name = types.StringValue(response.Nsg.Name)
	}
	if isUnset(model.Description) {
		// The API stores a null description. The schema defaults to the empty
		// string, so an import must land on the same value a create does.
		if response.Nsg.Description == nil {
			model.Description = types.StringValue("")
		} else {
			model.Description = types.StringValue(*response.Nsg.Description)
		}
	}
	if model.Rules.IsNull() || model.Rules.IsUnknown() {
		rules, ruleDiags := RulesToSet(ctx, response.Rules)
		diags.Append(ruleDiags...)
		model.Rules = rules
	}

	return model, diags
}

// RefreshNsgModelFromResponse shows what Terraform reconciles in place: the
// name, and the rule set this resource exists to manage. Read calls this after
// the mapper; Create and Update keep the planned values.
func RefreshNsgModelFromResponse(ctx context.Context, response *NsgDetail, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if response.Nsg.Name != "" {
		model.Name = types.StringValue(response.Nsg.Name)
	}

	rules, ruleDiags := RulesToSet(ctx, response.Rules)
	diags.Append(ruleDiags...)
	if !diags.HasError() {
		model.Rules = rules
	}

	return model, diags
}
