package provider

import (
	"context"
	"fmt"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// handlePartialCreate writes the resource that the API created to the state, and
// adds an error diagnostic. It returns false if err has no PartialCreateError.
//
// If the provider sets no state, the resource is lost: it exists, the account
// pays for it, but no later plan, refresh, or destroy can find it. The run is
// still an error, therefore Terraform marks the resource and replaces it.
func handlePartialCreate(ctx context.Context, err error, plan tfsdk.Plan, resp *resource.CreateResponse, summary string) bool {
	resourceID, ok := client.PartialCreateResourceID(err)
	if !ok {
		return false
	}

	tflog.Warn(ctx, "the create did not complete; the provider writes the resource ID to the state",
		map[string]any{"resource_id": resourceID, "error": err.Error()})

	resp.Diagnostics.Append(setPartialCreateState(ctx, plan, &resp.State, resourceID)...)
	resp.Diagnostics.AddError(
		summary,
		fmt.Sprintf("%s\n\nThe API created the resource with ID %q. The provider wrote that ID to the state, "+
			"so that the resource does not stay outside of Terraform. The resource can be incomplete. Do "+
			"'terraform apply' again to complete it, or 'terraform destroy' to remove it.", err.Error(), resourceID),
	)
	return true
}

// setPartialCreateState writes the plan to the state with the given resource ID.
//
// The plan is the only source of data: an interrupted create cannot send more
// requests. This function makes each unknown attribute null, because Terraform
// rejects a final state that is not fully known.
func setPartialCreateState(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, resourceID string) diag.Diagnostics {
	var diags diag.Diagnostics

	known, err := tftypes.Transform(plan.Raw, func(_ *tftypes.AttributePath, value tftypes.Value) (tftypes.Value, error) {
		if !value.IsKnown() {
			return tftypes.NewValue(value.Type(), nil), nil
		}
		return value, nil
	})
	if err != nil {
		diags.AddError(
			"Unable to record a partially created resource",
			fmt.Sprintf("The API created the resource with ID %q, but the provider cannot write it to the "+
				"state: %s. The resource exists outside of Terraform. Import it or remove it manually.", resourceID, err),
		)
		return diags
	}

	state.Raw = known
	diags.Append(state.SetAttribute(ctx, path.Root("id"), resourceID)...)
	return diags
}
