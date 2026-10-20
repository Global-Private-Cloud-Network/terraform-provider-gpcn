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

// RuleModel is one `rule` block. The API's rule ID is absent on purpose. GPCN
// ignores it on write. A set keyed on it churns on every replace.
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

// FailedNsgWarning reports a group GPCN gave up on. The row reads back
// cleanly, so the apply says nothing. The operator then holds a group whose
// rules never reached the fabric.
func FailedNsgWarning(response *NsgDetail) diag.Diagnostics {
	var diags diag.Diagnostics
	if response.Nsg.State != StateFailed {
		return diags
	}

	if response.Nsg.FailureReason == nil || *response.Nsg.FailureReason == "" {
		diags.AddWarning(WarnSummaryNsgFailed, fmt.Sprintf(WarnDetailNsgFailedNoReason, response.Nsg.ID))
		return diags
	}
	diags.AddWarning(WarnSummaryNsgFailed, fmt.Sprintf(WarnDetailNsgFailed, response.Nsg.ID, *response.Nsg.FailureReason))
	return diags
}

// DefaultNsgRulesWarning reports what a replace costs on the VPC's own default
// group. GPCN stages the platform posture there as ordinary rule rows. The
// rules route holds no guard, so the configured set replaces them.
func DefaultNsgRulesWarning(isDefault bool, nsgID string) diag.Diagnostics {
	var diags diag.Diagnostics
	if isDefault {
		diags.AddWarning(WarnSummaryDefaultNsgRulesReplaced, fmt.Sprintf(WarnDetailDefaultNsgRulesReplaced, nsgID))
	}
	return diags
}

// IsVpnOwned reports whether a site-to-site VPN connection owns the rule.
func (r ApiRule) IsVpnOwned() bool {
	return r.VpnConnectionID != nil && *r.VpnConnectionID != ""
}

// ruleContentKey is the identity the API gives a rule: direction, protocol,
// both ports and the remote CIDR. The description is not part of it.
func ruleContentKey(direction, protocol string, portMin, portMax *int64, remoteCidr string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", direction, protocol, portKey(portMin), portKey(portMax), remoteCidr)
}

func portKey(port *int64) string {
	if port == nil {
		return ""
	}
	return fmt.Sprintf("%d", *port)
}

func (r ApiRule) contentKey() string {
	return ruleContentKey(r.Direction, r.Protocol, r.PortRangeMin, r.PortRangeMax, r.RemoteCidr)
}

func (r RuleModel) contentKey() string {
	var portMin, portMax *int64
	if !r.PortRangeMin.IsNull() && !r.PortRangeMin.IsUnknown() {
		portMin = r.PortRangeMin.ValueInt64Pointer()
	}
	if !r.PortRangeMax.IsNull() && !r.PortRangeMax.IsUnknown() {
		portMax = r.PortRangeMax.ValueInt64Pointer()
	}
	return ruleContentKey(r.Direction.ValueString(), r.Protocol.ValueString(), portMin, portMax, r.RemoteCidr.ValueString())
}

// apiRuleBody renders a rule read from the API as a write body. It leaves out
// the owner, which the API ignores on write, and the null keys.
func apiRuleBody(rule ApiRule) map[string]any {
	body := map[string]any{
		"direction":  rule.Direction,
		"protocol":   rule.Protocol,
		"remoteCidr": rule.RemoteCidr,
	}
	if rule.PortRangeMin != nil {
		body["portRangeMin"] = *rule.PortRangeMin
	}
	if rule.PortRangeMax != nil {
		body["portRangeMax"] = *rule.PortRangeMax
	}
	if rule.Description != nil {
		body["description"] = *rule.Description
	}
	return body
}

// MergeVpnRuleBodies builds the rules replace body: the configured rules plus
// every rule a VPN connection owns. A configured rule with an owned rule's
// content and a different description wins, because the customer takes the rule
// over. A configured rule that matches an owned rule exactly, description
// included, stays the connection's. The second result is the owned rules that
// stay owned. A configured copy that differs only in description warns, because
// the connection stops managing that rule.
func MergeVpnRuleBodies(config []RuleModel, live []ApiRule) ([]map[string]any, []ApiRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	bodies := RuleRequestBodies(config)
	configured := make(map[string]RuleModel, len(config))
	for _, rule := range config {
		configured[rule.contentKey()] = rule
	}

	var resent []ApiRule
	for _, rule := range live {
		if !rule.IsVpnOwned() {
			continue
		}
		if declared, taken := configured[rule.contentKey()]; taken {
			if rule.matchesExactly(declared) {
				resent = append(resent, rule)
			} else {
				diags.AddWarning(WarnSummaryNsgVpnRuleOwnerChanged, fmt.Sprintf(WarnDetailNsgVpnRuleCopyDiffers, rule.RemoteCidr, *rule.VpnConnectionID))
			}
			continue
		}
		bodies = append(bodies, apiRuleBody(rule))
		resent = append(resent, rule)
	}
	return bodies, resent, diags
}

// matchesExactly reports whether a declared rule has the rule's content and
// its description.
func (r ApiRule) matchesExactly(declared RuleModel) bool {
	if r.contentKey() != declared.contentKey() {
		return false
	}
	declaredAbsent := declared.Description.IsNull() || declared.Description.IsUnknown()
	if r.Description == nil || declaredAbsent {
		return r.Description == nil && declaredAbsent
	}
	return *r.Description == declared.Description.ValueString()
}

// VpnOwnershipLostWarnings warns for each re-sent rule that the group now
// shows without an owner. The API relabels a rule to the customer when a
// rename of the connection races the replace. A connection deleted, or a peer
// network dropped, between the read and the replace has the same effect: the
// resend puts the released rule back as the group's own.
func VpnOwnershipLostWarnings(resent []ApiRule, after []ApiRule) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, sent := range resent {
		for _, rule := range after {
			if rule.contentKey() == sent.contentKey() && !rule.IsVpnOwned() {
				diags.AddWarning(WarnSummaryNsgVpnRuleOwnerChanged, fmt.Sprintf(WarnDetailNsgVpnRuleOwnerChanged, sent.RemoteCidr, *sent.VpnConnectionID))
				break
			}
		}
	}
	return diags
}

// RulesToSet converts the API's rules into the set the schema declares. A rule
// a VPN connection owns stays out, because the connection manages it. It stays
// in only when a declared rule matches it exactly, so a configuration that
// copies the rule plans no change.
func RulesToSet(ctx context.Context, rules []ApiRule, declared []RuleModel) (types.Set, diag.Diagnostics) {
	models := make([]RuleModel, 0, len(rules))
	for _, rule := range rules {
		if rule.IsVpnOwned() && !matchesAny(rule, declared) {
			continue
		}
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

func matchesAny(rule ApiRule, declared []RuleModel) bool {
	for _, candidate := range declared {
		if rule.matchesExactly(candidate) {
			return true
		}
	}
	return false
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

// RuleRequestBodies builds the payload the create and the replace both send.
// The body leaves out a null port or a null description. The API reads an
// absent key and a null one the same way. The body is also strict about the
// keys it does not know.
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

// MapIssuedNsgToModel records the ID the 202 answered with. The build job has
// not run, so nothing about the group is read and every other Computed
// attribute is null. The rules stay as the configuration wrote them.
func MapIssuedNsgToModel(nsgID string, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(nsgID)
	model.IsDefault = types.BoolNull()
	model.State = types.StringNull()
	model.FailureReason = types.StringNull()
	model.RuleCount = types.Int64Null()
	model.SubnetCount = types.Int64Null()
	model.CreatedTime = types.StringNull()
	model.LastUpdated = types.StringNull()
	return model
}

// MapNsgResponseToModel writes the Computed attributes. It fills the
// configurable ones only when the caller chose no value. An import and a
// Create both leave that behind.
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
		rules, ruleDiags := RulesToSet(ctx, response.Rules, nil)
		diags.Append(ruleDiags...)
		model.Rules = rules
	}

	return model, diags
}

// RefreshNsgModelFromResponse shows what Terraform reconciles in place: the
// name, the description, and the rule set this resource exists to manage. Read
// calls this after the mapper; Create and Update keep the planned values.
func RefreshNsgModelFromResponse(ctx context.Context, response *NsgDetail, model ResourceModel) (ResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if response.Nsg.Name != "" {
		model.Name = types.StringValue(response.Nsg.Name)
	}
	if response.Nsg.Description == nil {
		model.Description = types.StringValue("")
	} else {
		model.Description = types.StringValue(*response.Nsg.Description)
	}

	declared, declaredDiags := RulesFromSet(ctx, model.Rules)
	diags.Append(declaredDiags...)
	rules, ruleDiags := RulesToSet(ctx, response.Rules, declared)
	diags.Append(ruleDiags...)
	if !diags.HasError() {
		model.Rules = rules
	}

	return model, diags
}
