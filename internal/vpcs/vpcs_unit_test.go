package vpcs

import (
	"context"
	"encoding/json"
	"testing"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	vpcUnitTestID      = "vpc-1"
	vpcUnitTestCreated = "2026-01-02T15:04:05Z"
	vpcUnitTestUpdated = "2026-03-04T05:06:07Z"
)

func vpcUnitTestPayload() vpcPayload {
	description := "shared services"
	payload := vpcPayload{
		ID:             vpcUnitTestID,
		Name:           "vpc-from-api",
		Description:    &description,
		CIDR:           "10.50.0.0/16",
		Status:         VPC_STATUS_ACTIVE,
		CreatedAt:      vpcUnitTestCreated,
		UpdatedAt:      vpcUnitTestUpdated,
		DnsNameservers: []string{"8.8.8.8", "1.1.1.1"},
	}
	payload.Datacenter.ID = "dc-1"
	payload.Datacenter.Name = "Kansas"
	return payload
}

func stringList(t *testing.T, values ...string) types.List {
	t.Helper()

	elements := make([]attr.Value, len(values))
	for i, value := range values {
		elements[i] = types.StringValue(value)
	}
	list, diags := types.ListValue(types.StringType, elements)
	if diags.HasError() {
		t.Fatalf("Expected a list, got %v", diags)
	}
	return list
}

// An import arrives with the id alone. Every other attribute comes off the
// detail read. The three nullable strings stay null, because an empty string
// reads as a reported value.
func TestMapVpcResponseToModelFillsAnImportedVpc(t *testing.T) {
	t.Parallel()

	response := &readVpcResponse{Data: vpcUnitTestPayload()}
	model := MapVpcResponseToModel(context.Background(), response, ResourceModel{
		ID:             types.StringValue(vpcUnitTestID),
		DNSNameservers: types.ListNull(types.StringType),
	})

	if got := model.Name.ValueString(); got != "vpc-from-api" {
		t.Errorf("Name = %q, want %q", got, "vpc-from-api")
	}
	if got := model.CIDR.ValueString(); got != "10.50.0.0/16" {
		t.Errorf("CIDR = %q, want %q", got, "10.50.0.0/16")
	}
	if got := model.DatacenterId.ValueString(); got != "dc-1" {
		t.Errorf("DatacenterId = %q, want %q", got, "dc-1")
	}
	if got := model.Description.ValueString(); got != "shared services" {
		t.Errorf("Description = %q, want %q", got, "shared services")
	}
	if got := model.Status.ValueString(); got != VPC_STATUS_ACTIVE {
		t.Errorf("Status = %q, want %q", got, VPC_STATUS_ACTIVE)
	}
	if !model.EgressIp.IsNull() {
		t.Errorf("EgressIp = %v, want null", model.EgressIp)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("FailureReason = %v, want null", model.FailureReason)
	}
	if !model.ActiveJobId.IsNull() {
		t.Errorf("ActiveJobId = %v, want null", model.ActiveJobId)
	}
	if got := model.CreatedTime.ValueString(); got != "Friday, 02-Jan-26 15:04:05 UTC" {
		t.Errorf("CreatedTime = %q, want %q", got, "Friday, 02-Jan-26 15:04:05 UTC")
	}
	if got := model.LastUpdated.ValueString(); got != "Wednesday, 04-Mar-26 05:06:07 UTC" {
		t.Errorf("LastUpdated = %q, want %q", got, "Wednesday, 04-Mar-26 05:06:07 UTC")
	}
	want := stringList(t, "8.8.8.8", "1.1.1.1")
	if !model.DNSNameservers.Equal(want) {
		t.Errorf("DNSNameservers = %v, want %v", model.DNSNameservers, want)
	}
}

// The API stores a missing description as null. The schema defaults it to the
// empty string, so a null that reaches state plans a change forever.
func TestMapVpcResponseToModelNormalisesNullDescription(t *testing.T) {
	t.Parallel()

	payload := vpcUnitTestPayload()
	payload.Description = nil
	response := &readVpcResponse{Data: payload}

	model := MapVpcResponseToModel(context.Background(), response, ResourceModel{})
	if model.Description.IsNull() {
		t.Fatalf("Description = null, want an empty string")
	}
	if got := model.Description.ValueString(); got != "" {
		t.Errorf("Description = %q, want %q", got, "")
	}
}

// Reconciling any of these three plans a replacement, so a read must leave the
// configured value alone.
func TestMapVpcResponseToModelKeepsReplaceForcingValues(t *testing.T) {
	t.Parallel()

	configured := ResourceModel{
		CIDR:           types.StringValue("10.60.0.0/16"),
		DatacenterId:   types.StringValue("dc-2"),
		DNSNameservers: stringList(t, "9.9.9.9"),
	}
	response := &readVpcResponse{Data: vpcUnitTestPayload()}

	model := MapVpcResponseToModel(context.Background(), response, configured)
	if got := model.CIDR.ValueString(); got != "10.60.0.0/16" {
		t.Errorf("CIDR = %q, want %q", got, "10.60.0.0/16")
	}
	if got := model.DatacenterId.ValueString(); got != "dc-2" {
		t.Errorf("DatacenterId = %q, want %q", got, "dc-2")
	}
	want := stringList(t, "9.9.9.9")
	if !model.DNSNameservers.Equal(want) {
		t.Errorf("DNSNameservers = %v, want %v", model.DNSNameservers, want)
	}
}

// Terraform reconciles a rename in place, so the name is the one configured
// attribute a read refreshes.
func TestRefreshVpcModelFromResponseRefreshesNameOnly(t *testing.T) {
	t.Parallel()

	configured := ResourceModel{
		Name:         types.StringValue("vpc-configured"),
		CIDR:         types.StringValue("10.60.0.0/16"),
		DatacenterId: types.StringValue("dc-2"),
	}
	response := &readVpcResponse{Data: vpcUnitTestPayload()}

	model := RefreshVpcModelFromResponse(response, configured)
	if got := model.Name.ValueString(); got != "vpc-from-api" {
		t.Errorf("Name = %q, want %q", got, "vpc-from-api")
	}
	if got := model.CIDR.ValueString(); got != "10.60.0.0/16" {
		t.Errorf("CIDR = %q, want %q", got, "10.60.0.0/16")
	}
	if got := model.DatacenterId.ValueString(); got != "dc-2" {
		t.Errorf("DatacenterId = %q, want %q", got, "dc-2")
	}
}

// httpErrorFromBody builds the error the transport builds, so the details carry
// the types a JSON decode produces.
func httpErrorFromBody(t *testing.T, status int, code, message, details string) *client.HTTPError {
	t.Helper()

	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(details), &decoded); err != nil {
		t.Fatalf("Expected details to decode, got %v", err)
	}
	return &client.HTTPError{StatusCode: status, Code: code, Message: message, Details: decoded}
}

func TestVpcCreateFailureDiagnosticRendersOverlap(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_CIDR_OVERLAP_UNCONFIRMED,
		`CIDR overlaps existing VPC(s): "web" (10.50.0.0/16). Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed.`,
		`{"overlapping":[{"id":"vpc-2","name":"web","cidr":"10.50.0.0/16"},{"id":"vpc-3","name":"data","cidr":"10.50.128.0/17"}],"hiddenOverlapCount":0}`)

	diagnostic := CreateFailureDiagnostic(err)
	if got := diagnostic.Severity(); got != diag.SeverityError {
		t.Errorf("Severity = %v, want %v", got, diag.SeverityError)
	}
	if got := diagnostic.Summary(); got != "VPC CIDR overlaps an existing VPC" {
		t.Errorf("Summary = %q, want %q", got, "VPC CIDR overlaps an existing VPC")
	}
	want := `HTTP 409 (VPC_CIDR_OVERLAP_UNCONFIRMED): CIDR overlaps existing VPC(s): "web" (10.50.0.0/16). Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed. Overlapping VPCs: web (10.50.0.0/16), data (10.50.128.0/17). Set acknowledge_overlap = true to proceed.`
	if got := diagnostic.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// The API names only the VPCs inside the reader's resource groups and counts
// the rest. A list that drops the count names fewer VPCs than the sentence
// above it totals.
func TestVpcCreateFailureDiagnosticCountsHiddenOverlaps(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_CIDR_OVERLAP_UNCONFIRMED,
		`CIDR overlaps existing VPC(s): "web" (10.50.0.0/16) and 2 VPC(s) outside your resource groups. Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed.`,
		`{"overlapping":[{"id":"vpc-2","name":"web","cidr":"10.50.0.0/16"}],"hiddenOverlapCount":2}`)

	want := `HTTP 409 (VPC_CIDR_OVERLAP_UNCONFIRMED): CIDR overlaps existing VPC(s): "web" (10.50.0.0/16) and 2 VPC(s) outside your resource groups. Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed. Overlapping VPCs: web (10.50.0.0/16) and 2 more in resource groups you cannot see. Set acknowledge_overlap = true to proceed.`
	if got := CreateFailureDiagnostic(err).Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// The API names only the VPCs inside the reader's resource groups. A refusal
// that names none still tells the reader how to proceed.
func TestVpcCreateFailureDiagnosticWithNoVisibleOverlap(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_CIDR_OVERLAP_UNCONFIRMED,
		`CIDR overlaps existing VPC(s): 2 VPC(s) outside your resource groups. Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed.`,
		`{"overlapping":[],"hiddenOverlapCount":2}`)

	want := `HTTP 409 (VPC_CIDR_OVERLAP_UNCONFIRMED): CIDR overlaps existing VPC(s): 2 VPC(s) outside your resource groups. Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed. Set acknowledge_overlap = true to proceed.`
	if got := CreateFailureDiagnostic(err).Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// Any other create failure is the API's own sentence.
func TestVpcCreateFailureDiagnosticForwardsOtherRefusals(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, "VPC_NAME_ALREADY_TAKEN",
		`A VPC named "shared" already exists in this data center`, `{}`)

	diagnostic := CreateFailureDiagnostic(err)
	if got := diagnostic.Summary(); got != "Unable to create GPCN VPC" {
		t.Errorf("Summary = %q, want %q", got, "Unable to create GPCN VPC")
	}
	want := `HTTP 409 (VPC_NAME_ALREADY_TAKEN): A VPC named "shared" already exists in this data center`
	if got := diagnostic.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

func TestVpcDeleteFailureDiagnosticRendersCensus(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_VPC_NOT_EMPTY,
		`Cannot delete VPC with 3 subnet(s), 1 network security group(s). Delete subnets and network security groups and release public IPs first.`,
		`{"blockers":{"subnets":2,"publicIps":0,"nsgs":1},"inFlight":{"subnets":1,"publicIps":0,"nsgs":0}}`)

	diagnostic := DeleteFailureDiagnostic(vpcUnitTestID, err)
	if got := diagnostic.Summary(); got != "Unable to delete GPCN VPC" {
		t.Errorf("Summary = %q, want %q", got, "Unable to delete GPCN VPC")
	}
	want := `HTTP 409 (VPC_NOT_EMPTY): Cannot delete VPC with 3 subnet(s), 1 network security group(s). Delete subnets and network security groups and release public IPs first. Blockers: 2 subnet(s), 1 network security group(s). In flight: 1 subnet(s).`
	if got := diagnostic.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// A refusal that names only in-flight rows leaves the reader nothing to delete.
func TestVpcDeleteFailureDiagnosticRendersEmptyBlockers(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_VPC_NOT_EMPTY,
		`Cannot delete VPC with 1 public IP(s). Wait for the in-progress operations on them to finish, then retry.`,
		`{"blockers":{"subnets":0,"publicIps":0,"nsgs":0},"inFlight":{"subnets":0,"publicIps":1,"nsgs":0}}`)

	want := `HTTP 409 (VPC_NOT_EMPTY): Cannot delete VPC with 1 public IP(s). Wait for the in-progress operations on them to finish, then retry. Blockers: none. In flight: 1 public IP(s).`
	if got := DeleteFailureDiagnostic(vpcUnitTestID, err).Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

func TestVpcDeleteFailureDiagnosticForwardsOtherRefusals(t *testing.T) {
	t.Parallel()

	err := httpErrorFromBody(t, 409, ERROR_CODE_VPC_NOT_ACTIVE,
		`The VPC is not active (status: deleting); this operation needs an active VPC.`, `{}`)

	want := `Unable to delete GPCN VPC with ID 'vpc-1': HTTP 409 (VPC_NOT_ACTIVE): The VPC is not active (status: deleting); this operation needs an active VPC.`
	if got := DeleteFailureDiagnostic(vpcUnitTestID, err).Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

func TestFailedVpcWarningNamesTheReason(t *testing.T) {
	t.Parallel()

	warning := FailedVpcWarning(vpcUnitTestID, "the anchor router never came up")
	if got := warning.Severity(); got != diag.SeverityWarning {
		t.Errorf("Severity = %v, want %v", got, diag.SeverityWarning)
	}
	if got := warning.Summary(); got != "VPC is in the failed state" {
		t.Errorf("Summary = %q, want %q", got, "VPC is in the failed state")
	}
	want := "VPC vpc-1 is in the failed state: the anchor router never came up. Destroy the VPC and create it again."
	if got := warning.Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

func TestFailedVpcWarningWithoutAReason(t *testing.T) {
	t.Parallel()

	want := "VPC vpc-1 is in the failed state. Destroy the VPC and create it again."
	if got := FailedVpcWarning(vpcUnitTestID, "").Detail(); got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}
