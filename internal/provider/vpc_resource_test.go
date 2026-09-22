package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/helpers"
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

// vpcAccOctetSlot names the block each acceptance case in this file owns. A
// case that draws its block at random meets an earlier run about half the
// time. The repository has no sweepers.
var vpcAccOctetSlot = map[string]int{
	"TestVpcResource": 0,
}

// This file owns the window at base 64. The subnet, group and machine files
// own 80, 96 and 112.
const (
	vpcAccOctetBase  = 64
	vpcAccOctetSlots = 16
)

// vpcAccOctetFor returns the octet of the named case. It fails a case the
// table does not name.
func vpcAccOctetFor(t testing.TB, name string) int {
	t.Helper()
	slot, named := vpcAccOctetSlot[name]
	if !named {
		t.Fatalf("Expected %s to own a slot in vpcAccOctetSlot", name)
	}
	return vpcAccOctetBase + slot
}

// The guard reads the table alone, not the call sites. It refuses a slot
// outside this file's window, because that slot takes a block this file
// does not own. It refuses two entries that take one octet. The compiler
// accepts both.
func TestVpcAcceptanceOctetsAreUnique(t *testing.T) {
	owner := map[int]string{}
	for name, slot := range vpcAccOctetSlot {
		if slot < 0 || slot >= vpcAccOctetSlots {
			t.Errorf("Expected %s to take a slot below %d, got %d", name, vpcAccOctetSlots, slot)
		}
		octet := vpcAccOctetFor(t, name)
		if other, taken := owner[octet]; taken {
			t.Errorf("Expected %s and %s to take different octets, both take %d", name, other, octet)
		}
		owner[octet] = name
	}
}

func TestVpcResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	vpcName := fmt.Sprintf("vpc-basic-%s", rName)
	vpcNameUpdated := fmt.Sprintf("vpc-basic-updated-%s", rName)
	// The overlap check covers the whole entity. A fixed block meets only what
	// an earlier run of this case left behind, which the configuration
	// acknowledges.
	vpcCidr := fmt.Sprintf("10.%d.0.0/16", vpcAccOctetFor(t, t.Name()))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				name        = "Chicago"
				vpc_capable = true
			}

			resource "gpcn_vpc" "test" {
				name                = "%s"
				datacenter_id       = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr                = "%s"
				description         = "terraform acceptance"
				acknowledge_overlap = true
			}
			`, vpcName, vpcCidr),
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
					resource.TestCheckResourceAttr(gpcnVpcTest, "cidr", vpcCidr),
					resource.TestCheckResourceAttr(gpcnVpcTest, "description", "terraform acceptance"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "status", "active"),
				),
			},
			// ImportState testing
			{
				ResourceName:      gpcnVpcTest,
				ImportState:       true,
				ImportStateVerify: true,
				// The API never answers with acknowledge_overlap, so an import
				// leaves it null while the configuration sets it.
				ImportStateVerifyIgnore: []string{"created_time", "last_updated", "acknowledge_overlap"},
			},
			// Update and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				name        = "Chicago"
				vpc_capable = true
			}

			resource "gpcn_vpc" "test" {
				name                = "%s"
				datacenter_id       = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr                = "%s"
				description         = "terraform acceptance, renamed"
				acknowledge_overlap = true
			}
			`, vpcNameUpdated, vpcCidr),
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
	NewVPCResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}
	return schemaResponse.Schema
}

// assertWhitespaceValidator reports whether the attribute carries the shared
// whitespace refusal with the summary its resource names. The fold moved the
// validator out of the resource packages, so the wiring needs a guard.
func assertWhitespaceValidator(t *testing.T, attributes map[string]schema.Attribute, attributeName, wantSummary string) {
	t.Helper()

	attribute, ok := attributes[attributeName].(schema.StringAttribute)
	if !ok {
		t.Fatalf("%s is %T, want schema.StringAttribute", attributeName, attributes[attributeName])
	}
	for _, candidate := range attribute.Validators {
		whitespace, isWhitespace := candidate.(helpers.NoOuterWhitespaceValidator)
		if isWhitespace && whitespace.Attribute == attributeName && whitespace.Summary == wantSummary {
			return
		}
	}
	t.Errorf("%s carries %d validators, none of them a NoOuterWhitespaceValidator for %q with the summary %q", attributeName, len(attribute.Validators), attributeName, wantSummary)
}

// The shared validator changes nothing until the schema carries it.
func TestVpcResourceSchemaAttachesWhitespaceValidators(t *testing.T) {
	t.Parallel()

	attributes := vpcResourceTestSchema(t).Attributes
	assertWhitespaceValidator(t, attributes, "name", "Invalid VPC %s")
	assertWhitespaceValidator(t, attributes, "description", "Invalid VPC %s")
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

	readInStatus := func(status string, failureReason any) fwresource.ReadResponse {
		t.Helper()

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
						"status":         status,
						"failureReason":  failureReason,
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
		return *readResponse
	}

	readWithFailureReason := func(failureReason any) diag.Diagnostic {
		t.Helper()

		readResponse := readInStatus("failed", failureReason)
		if count := len(readResponse.Diagnostics); count != 1 {
			t.Fatalf("Expected 1 diagnostic, got %d: %v", count, readResponse.Diagnostics)
		}
		return readResponse.Diagnostics[0]
	}

	warning := readWithFailureReason("the anchor router never came up")
	if warning.Severity() != diag.SeverityWarning {
		t.Errorf("Severity = %v, want %v", warning.Severity(), diag.SeverityWarning)
	}
	if got := warning.Summary(); got != "VPC is in the failed state" {
		t.Errorf("Summary = %q, want %q", got, "VPC is in the failed state")
	}
	want := "VPC vpc-1 is in the failed state: the anchor router never came up. Destroy the VPC and create it again."
	if got := warning.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}

	// The platform can park a VPC with no reason recorded.
	noReason := readWithFailureReason(nil)
	wantNoReason := "VPC vpc-1 is in the failed state. Destroy the VPC and create it again."
	if got := noReason.Detail(); got != wantNoReason {
		t.Errorf("Detail with no reason = %q, want %q", got, wantNoReason)
	}

	// An active VPC must stay silent. Without this drive an unconditional
	// warning passes every assertion above.
	active := readInStatus("active", nil)
	if quiet := active.Diagnostics.Warnings(); len(quiet) != 0 {
		t.Errorf("Warnings on an active VPC = %v, want none", quiet)
	}
}

// gpcn_vpc is a resource, so its Configure refusal must say so. The other five
// Release B resources say Resource. Two wordings for one refusal confuse the
// reader.
func TestVpcResourceConfigureRefusesAnotherProviderData(t *testing.T) {
	t.Parallel()

	response := &fwresource.ConfigureResponse{}
	(&vpcResource{}).Configure(
		context.Background(),
		fwresource.ConfigureRequest{ProviderData: "not a client"},
		response,
	)

	errors := response.Diagnostics.Errors()
	if len(errors) != 1 {
		t.Fatalf("Diagnostics = %v, want exactly one error", response.Diagnostics)
	}
	if got := errors[0].Summary(); got != "Unexpected Resource Configure Type" {
		t.Errorf("Summary = %q, want %q", got, "Unexpected Resource Configure Type")
	}
	want := "Expected *client.GpcnClient, got: string. Please report this issue to the provider developers."
	if got := errors[0].Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// The Registry prints the resource Description as prose. A sentence that stops
// without a full stop runs into the heading below it.
func TestVpcResourceSchemaDescriptionBytes(t *testing.T) {
	t.Parallel()

	want := "Manages a VPC, the routed private network that holds subnets, security groups and public IP addresses in one datacenter. The API key needs vpc:read, vpc:create, vpc:update and vpc:delete."
	if got := vpcResourceTestSchema(t).Description; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}
