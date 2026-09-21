package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/vpcs"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestVpcResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcName := fmt.Sprintf("vpc-basic-%s", rName)
	vpcNameUpdated := fmt.Sprintf("vpc-basic-updated-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_vpc" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr          = "10.60.0.0/16"
				description   = "terraform acceptance"
			}
			`, vpcName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVpcTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVpcTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVpcTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVpcTest, "dns_nameservers.0"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", vpcName),
					resource.TestCheckResourceAttr(gpcnVpcTest, "cidr", "10.60.0.0/16"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "description", "terraform acceptance"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "status", "active"),
				),
			},
			// ImportState testing
			{
				ResourceName:            gpcnVpcTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
			// Update and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_vpc" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr          = "10.60.0.0/16"
				description   = "terraform acceptance, renamed"
			}
			`, vpcNameUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", vpcNameUpdated),
					resource.TestCheckResourceAttr(gpcnVpcTest, "description", "terraform acceptance, renamed"),
				),
			},
		},
	})
}

/*
*
----- Unit tests -----
*
*/

// vpcResourceTestSchema returns the schema the framework state helpers need.
func vpcResourceTestSchema(t *testing.T) schema.Schema {
	t.Helper()

	schemaResponse := &fwresource.SchemaResponse{}
	NewVpcResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}
	return schemaResponse.Schema
}

// The create 202 carries the row, so a failed create job must still leave the
// VPC id in state. A run that loses it leaves a live VPC nothing manages.
func TestVpcResourceCreateWritesIdBeforePolling(t *testing.T) {
	t.Parallel()

	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data": map[string]any{
						"jobId": vpcPlanTestCreateJobID,
						"vpc": map[string]any{
							"id":             vpcPlanTestID,
							"name":           "vpc-orphan",
							"description":    "",
							"cidr":           vpcPlanTestCidr,
							"datacenter":     map[string]any{"id": vpcPlanTestDatacenterID, "code": "kansas", "name": "Kansas"},
							"status":         "creating",
							"failureReason":  nil,
							"egressIp":       nil,
							"activeJobId":    vpcPlanTestCreateJobID,
							"createdAt":      vpcPlanTestTimestamp,
							"updatedAt":      vpcPlanTestTimestamp,
							"dnsNameservers": []string{"8.8.8.8"},
						},
					},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Data: client.JobStatusDataResponse{Jobs: []client.JobResponse{{
						JobID:        vpcPlanTestCreateJobID,
						HasFailed:    true,
						IsTerminal:   true,
						ErrorMessage: "the anchor router never came up",
					}}},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	vpcSchema := vpcResourceTestSchema(t)
	plan := tfsdk.Plan{Schema: vpcSchema}
	diags := plan.Set(context.Background(), vpcs.ResourceModel{
		Name:           types.StringValue("vpc-orphan"),
		DatacenterId:   types.StringValue(vpcPlanTestDatacenterID),
		CIDR:           types.StringValue(vpcPlanTestCidr),
		Description:    types.StringValue(""),
		DNSNameservers: types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("Expected a plan, got %v", diags)
	}

	vpcResource := &vpcResource{client: gpcnClient}
	createResponse := &fwresource.CreateResponse{State: tfsdk.State{Schema: vpcSchema}}
	vpcResource.Create(context.Background(), fwresource.CreateRequest{Plan: plan}, createResponse)

	if !createResponse.Diagnostics.HasError() {
		t.Fatalf("Expected the failed create job to raise an error")
	}
	if createResponse.State.Raw.IsNull() {
		t.Fatalf("Expected the VPC to stay in state after the failed job")
	}

	var written vpcs.ResourceModel
	diags = createResponse.State.Get(context.Background(), &written)
	if diags.HasError() {
		t.Fatalf("Expected to read the written state, got %v", diags)
	}
	if got := written.ID.ValueString(); got != vpcPlanTestID {
		t.Errorf("ID = %q, want %q", got, vpcPlanTestID)
	}
}

// The acceptance harness cannot observe a warning diagnostic, so the failed
// read needs a direct call.
func TestVpcResourceReadWarnsWhenVpcFailed(t *testing.T) {
	t.Parallel()

	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id":             vpcPlanTestID,
					"name":           "vpc-failed",
					"description":    "",
					"cidr":           vpcPlanTestCidr,
					"datacenter":     map[string]any{"id": vpcPlanTestDatacenterID, "code": "kansas", "name": "Kansas"},
					"status":         "failed",
					"failureReason":  "the anchor router never came up",
					"egressIp":       nil,
					"activeJobId":    nil,
					"createdAt":      vpcPlanTestTimestamp,
					"updatedAt":      vpcPlanTestTimestamp,
					"dnsNameservers": []string{"8.8.8.8"},
				},
			})
		},
	})

	vpcSchema := vpcResourceTestSchema(t)
	priorState := tfsdk.State{Schema: vpcSchema}
	diags := priorState.Set(context.Background(), vpcs.ResourceModel{
		ID:             types.StringValue(vpcPlanTestID),
		Name:           types.StringValue("vpc-failed"),
		DatacenterId:   types.StringValue(vpcPlanTestDatacenterID),
		CIDR:           types.StringValue(vpcPlanTestCidr),
		Description:    types.StringValue(""),
		DNSNameservers: types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("Expected a prior state, got %v", diags)
	}

	vpcResource := &vpcResource{client: gpcnClient}
	readResponse := &fwresource.ReadResponse{State: priorState}
	vpcResource.Read(context.Background(), fwresource.ReadRequest{State: priorState}, readResponse)

	if readResponse.Diagnostics.HasError() {
		t.Fatalf("Expected no error diagnostic, got %v", readResponse.Diagnostics)
	}
	if count := len(readResponse.Diagnostics); count != 1 {
		t.Fatalf("Expected 1 diagnostic, got %d: %v", count, readResponse.Diagnostics)
	}
	warning := readResponse.Diagnostics[0]
	if warning.Severity() != diag.SeverityWarning {
		t.Errorf("Severity = %v, want %v", warning.Severity(), diag.SeverityWarning)
	}
	if got := warning.Summary(); got != vpcs.WarnSummaryVpcFailed {
		t.Errorf("Summary = %q, want %q", got, vpcs.WarnSummaryVpcFailed)
	}
	want := fmt.Sprintf(vpcs.WarnDetailVpcFailed, vpcPlanTestID, "the anchor router never came up")
	if got := warning.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}
