package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// networkCreatePlan makes the plan that the framework gives to Create. The
// configured attributes are known. The computed attributes are unknown.
func networkCreatePlan(t *testing.T, ctx context.Context) (tfsdk.Plan, tfsdk.State) {
	t.Helper()

	var schemaResp resource.SchemaResponse
	(&networksResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("building the network schema failed: %v", schemaResp.Diagnostics)
	}
	schema := schemaResp.Schema

	objectType, ok := schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", schema.Type().TerraformType(ctx))
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attrType := range objectType.AttributeTypes {
		if name == "name" {
			values[name] = tftypes.NewValue(attrType, "example-network")
			continue
		}
		values[name] = tftypes.NewValue(attrType, tftypes.UnknownValue)
	}

	plan := tfsdk.Plan{Schema: schema, Raw: tftypes.NewValue(objectType, values)}
	state := tfsdk.State{Schema: schema, Raw: tftypes.NewValue(objectType, nil)}
	return plan, state
}

// A create can fail after the API makes the resource. If the provider returns an
// error and sets no state, the resource is lost: it exists, the account pays for
// it, but no later plan, refresh, or destroy can find it.
func TestHandlePartialCreateRecordsResourceID(t *testing.T) {
	ctx := context.Background()
	plan, state := networkCreatePlan(t, ctx)

	resp := &resource.CreateResponse{State: state}
	err := client.NewPartialCreateError("net-abc-123", errors.New("polling interrupted"))

	if !handlePartialCreate(ctx, err, plan, resp, "Unable to create GPCN Network") {
		t.Fatal("handlePartialCreate returned false for a partial create error")
	}

	if !resp.Diagnostics.HasError() {
		t.Error("expected the failure to still be reported as an error diagnostic")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "net-abc-123") {
		t.Errorf("diagnostic %q does not name the resource that was left behind", resp.Diagnostics.Errors()[0].Detail())
	}

	// Terraform rejects a final state that is not fully known.
	if !resp.State.Raw.IsFullyKnown() {
		t.Error("state still contains unknown values; Terraform would reject it")
	}

	var id types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &id); diags.HasError() {
		t.Fatalf("reading id back from state failed: %v", diags)
	}
	if id.ValueString() != "net-abc-123" {
		t.Errorf("state id = %q, want %q", id.ValueString(), "net-abc-123")
	}

	// The next run has only these values.
	var name types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("name"), &name); diags.HasError() {
		t.Fatalf("reading name back from state failed: %v", diags)
	}
	if name.ValueString() != "example-network" {
		t.Errorf("state name = %q, want the planned value to be preserved", name.ValueString())
	}
}

func TestHandlePartialCreateIgnoresOtherErrors(t *testing.T) {
	ctx := context.Background()
	plan, state := networkCreatePlan(t, ctx)

	resp := &resource.CreateResponse{State: state}
	if handlePartialCreate(ctx, errors.New("the create request itself failed"), plan, resp, "Unable to create GPCN Network") {
		t.Error("handlePartialCreate returned true for an error that reported no resource ID")
	}
	if resp.Diagnostics.HasError() {
		t.Error("handlePartialCreate added diagnostics for an error it does not handle")
	}
	if !resp.State.Raw.IsNull() {
		t.Error("handlePartialCreate wrote state for an error it does not handle")
	}
}
