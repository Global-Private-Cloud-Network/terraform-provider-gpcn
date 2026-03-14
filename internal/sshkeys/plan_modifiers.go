package sshkeys

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// PrivateKeyPlanModifier sets private_key to null in the plan when public_key is
// provided (upload mode), since the API never returns a private key in that case.
// In generate mode it falls back to UseStateForUnknown behavior.
type PrivateKeyPlanModifier struct{}

func (m PrivateKeyPlanModifier) Description(_ context.Context) string {
	return "Sets private_key to null when public_key is provided; otherwise preserves state."
}

func (m PrivateKeyPlanModifier) MarkdownDescription(_ context.Context) string {
	return "Sets `private_key` to null when `public_key` is provided; otherwise preserves state."
}

func (m PrivateKeyPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	var config ResourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	uploadMode := !config.PublicKey.IsNull() && config.PublicKey.ValueString() != ""

	if uploadMode {
		resp.PlanValue = types.StringNull()
		return
	}

	// Generate mode: preserve state so Terraform doesn't show "(known after apply)"
	// on refreshes after the initial creation.
	if !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
	}
}
