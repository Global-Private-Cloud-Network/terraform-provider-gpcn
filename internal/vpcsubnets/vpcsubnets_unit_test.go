package vpcsubnets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	unitTestVpcID     = "vpc-1"
	unitTestSubnetID  = "subnet-1"
	unitTestNsgID     = "nsg-1"
	unitTestTimestamp = "2026-01-02T15:04:05Z"
)

func unitTestRFC850(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("failed to parse the fixture timestamp %q: %v", raw, err)
	}
	return parsed.Format(time.RFC850)
}

func unitTestApiSubnet() *ApiSubnet {
	nsgName := "default"
	return &ApiSubnet{
		ID:               unitTestSubnetID,
		Name:             "subnet-a",
		Description:      nil,
		CIDR:             "10.50.1.0/24",
		State:            "ready",
		NsgID:            unitTestNsgID,
		NsgName:          &nsgName,
		AttachedNicCount: 2,
		FailureReason:    nil,
		CreatedAt:        unitTestTimestamp,
		UpdatedAt:        unitTestTimestamp,
	}
}

// A null description reaches Terraform as the empty string the schema defaults
// to. A null failure reason stays null, because a subnet that never failed has
// no reason to show.
func TestMapSubnetResponseToModelFillsComputedUnit(t *testing.T) {
	t.Parallel()

	model := MapSubnetResponseToModel(unitTestApiSubnet(), ResourceModel{})

	if got := model.ID.ValueString(); got != unitTestSubnetID {
		t.Errorf("expected id %q, got %q", unitTestSubnetID, got)
	}
	if got := model.Description.ValueString(); got != "" {
		t.Errorf("expected an empty description, got %q", got)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("expected a null failure_reason, got %q", model.FailureReason.ValueString())
	}
	if got := model.NsgName.ValueString(); got != "default" {
		t.Errorf("expected nsg_name %q, got %q", "default", got)
	}
	if got := model.AttachedNicCount.ValueInt64(); got != 2 {
		t.Errorf("expected attached_nic_count 2, got %d", got)
	}
	if got := model.State.ValueString(); got != "ready" {
		t.Errorf("expected state %q, got %q", "ready", got)
	}
	want := unitTestRFC850(t, unitTestTimestamp)
	if got := model.CreatedTime.ValueString(); got != want {
		t.Errorf("expected created_time %q, got %q", want, got)
	}
	if got := model.LastUpdated.ValueString(); got != want {
		t.Errorf("expected last_updated %q, got %q", want, got)
	}
}

// A drifted CIDR plans a destroy of a subnet that can hold live interfaces.
// The mapper must leave a value the configuration already carries.
func TestMapSubnetResponseToModelKeepsConfiguredCidrUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.CIDR = "10.50.9.0/24"

	model := MapSubnetResponseToModel(response, ResourceModel{
		CIDR:  types.StringValue("10.50.1.0/24"),
		Name:  types.StringValue("subnet-a"),
		NsgID: types.StringValue(unitTestNsgID),
	})

	if got := model.CIDR.ValueString(); got != "10.50.1.0/24" {
		t.Errorf("expected the configured cidr to survive the mapper, got %q", got)
	}
}

// Read shows a rename and a rebind, because Terraform reconciles both in place.
// The prefix is request-only and the CIDR is the allocator's reservation, so
// neither refreshes.
func TestRefreshSubnetModelFromResponseUpdatesNameAndNsgUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.Name = "renamed-out-of-band"
	response.NsgID = "nsg-2"
	response.CIDR = "10.50.9.0/24"

	model := RefreshSubnetModelFromResponse(response, ResourceModel{
		Name:   types.StringValue("subnet-a"),
		NsgID:  types.StringValue(unitTestNsgID),
		CIDR:   types.StringValue("10.50.1.0/24"),
		Prefix: types.Int64Value(24),
	})

	if got := model.Name.ValueString(); got != "renamed-out-of-band" {
		t.Errorf("expected the refreshed name, got %q", got)
	}
	if got := model.NsgID.ValueString(); got != "nsg-2" {
		t.Errorf("expected the refreshed nsg_id, got %q", got)
	}
	if got := model.CIDR.ValueString(); got != "10.50.1.0/24" {
		t.Errorf("expected the cidr to stay at the configured value, got %q", got)
	}
	if got := model.Prefix.ValueInt64(); got != 24 {
		t.Errorf("expected the prefix to stay at the configured value, got %d", got)
	}
}

// The subnet has no single-read endpoint, so Read pages the parent listing. A
// subnet on the last page must still be found.
func TestGetSubnetPagesListingUnit(t *testing.T) {
	t.Parallel()

	var pagesServed []string
	server, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			pagesServed = append(pagesServed, page)
			if page == "2" {
				testutil.WriteJSONResponse(w, unitTestListBody([]map[string]any{{
					"id":               unitTestSubnetID,
					"name":             "subnet-a",
					"cidr":             "10.50.1.0/24",
					"state":            "ready",
					"nsgId":            unitTestNsgID,
					"attachedNicCount": 0,
					"createdAt":        unitTestTimestamp,
					"updatedAt":        unitTestTimestamp,
				}}, 2))
				return
			}
			testutil.WriteJSONResponse(w, unitTestListBody([]map[string]any{{"id": "subnet-other"}}, 2))
		},
	})
	defer server.Close()

	subnet, err := GetSubnet(gpcnClient, context.Background(), unitTestVpcID, unitTestSubnetID)
	if err != nil {
		t.Fatalf("expected the paged listing to find the subnet, got %v", err)
	}
	if subnet.CIDR != "10.50.1.0/24" {
		t.Errorf("expected the row from page 2, got %+v", subnet)
	}
	if len(pagesServed) != 2 {
		t.Errorf("expected two pages to be requested, got %v", pagesServed)
	}
}

// A subnet missing from every page was deleted outside Terraform. Read removes
// it from state, so the absence needs an error the caller can recognize.
func TestGetSubnetAbsentUnit(t *testing.T) {
	t.Parallel()

	server, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			testutil.WriteJSONResponse(w, unitTestListBody([]map[string]any{}, 1))
		},
	})
	defer server.Close()

	_, err := GetSubnet(gpcnClient, context.Background(), unitTestVpcID, unitTestSubnetID)
	if !errors.Is(err, ErrSubnetAbsent) {
		t.Fatalf("expected ErrSubnetAbsent, got %v", err)
	}
}

func unitTestListBody(rows []map[string]any, totalPages int) map[string]any {
	return map[string]any{
		"success": true,
		"message": "",
		"data":    rows,
		"meta": map[string]any{
			"total":      len(rows),
			"page":       1,
			"pageSize":   ListPageLimit,
			"totalPages": totalPages,
		},
	}
}

// The update body carries the name and the description and no structural key.
// The API refuses an unknown key, and it needs at least one of the two.
func TestUpdateSubnetSendsNameAndDescriptionUnit(t *testing.T) {
	t.Parallel()

	var sentBody map[string]any
	var sentPath string
	server, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			sentPath = r.URL.Path
			sentBody = testutil.ReadRequestBody(r)
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"data": map[string]any{
					"id":        unitTestSubnetID,
					"name":      "subnet-b",
					"cidr":      "10.50.1.0/24",
					"state":     "ready",
					"nsgId":     unitTestNsgID,
					"createdAt": unitTestTimestamp,
					"updatedAt": unitTestTimestamp,
				},
			})
		},
	})
	defer server.Close()

	subnet, err := UpdateSubnet(gpcnClient, context.Background(), unitTestVpcID, unitTestSubnetID, "subnet-b", "new description")
	if err != nil {
		t.Fatalf("expected the update to succeed, got %v", err)
	}
	if subnet.Name != "subnet-b" {
		t.Errorf("expected the updated row, got %+v", subnet)
	}
	wantPath := fmt.Sprintf("%s%s/subnets/%s", BaseURLV1, unitTestVpcID, unitTestSubnetID)
	if sentPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, sentPath)
	}
	if got, _ := sentBody["name"].(string); got != "subnet-b" {
		t.Errorf("expected the name in the body, got %q", got)
	}
	if got, _ := sentBody["description"].(string); got != "new description" {
		t.Errorf("expected the description in the body, got %q", got)
	}
	if _, present := sentBody["cidr"]; present {
		t.Error("expected no cidr key in the update body")
	}
}

// A Computed attribute arrives at Create as unknown, not null. The mapper must
// fill it from the response, or the apply ends with an unknown value in state.
func TestMapSubnetResponseToModelFillsUnknownValuesUnit(t *testing.T) {
	t.Parallel()

	model := MapSubnetResponseToModel(unitTestApiSubnet(), ResourceModel{
		Description: types.StringUnknown(),
		CIDR:        types.StringUnknown(),
		NsgID:       types.StringUnknown(),
	})

	if model.NsgID.IsUnknown() || model.NsgID.ValueString() != unitTestNsgID {
		t.Errorf("expected nsg_id %q, got %v", unitTestNsgID, model.NsgID)
	}
	if model.CIDR.IsUnknown() || model.CIDR.ValueString() != "10.50.1.0/24" {
		t.Errorf("expected the cidr from the response, got %v", model.CIDR)
	}
	if model.Description.IsUnknown() || model.Description.ValueString() != "" {
		t.Errorf("expected an empty description, got %v", model.Description)
	}
}

// A failed carve leaves a row Terraform reads without complaint. The warning is
// the only place the operator learns the subnet carries no network.
func TestSubnetFailedWarningUnit(t *testing.T) {
	t.Parallel()

	reason := "the provider rejected the allocation"
	response := unitTestApiSubnet()
	response.State = StateFailed
	response.FailureReason = &reason

	diags := SubnetFailedWarning(response)

	if got := diags.WarningsCount(); got != 1 {
		t.Fatalf("expected one warning, got %d", got)
	}
	warning := diags.Warnings()[0]
	const wantSummary = "GPCN VPC subnet is in the failed state"
	if got := warning.Summary(); got != wantSummary {
		t.Errorf("expected the summary %q, got %q", wantSummary, got)
	}
	want := "Subnet 'subnet-1' is in the 'failed' state, so it carries no working network. GPCN gives this reason: the provider rejected the allocation. Delete the subnet and create it again, or contact GPCN support."
	if got := warning.Detail(); got != want {
		t.Errorf("expected the detail %q, got %q", want, got)
	}
}

// A failed subnet with no reason still needs the warning. The sentence must
// stay readable where the reason would have been.
func TestSubnetFailedWarningWithoutReasonUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.State = StateFailed

	diags := SubnetFailedWarning(response)

	want := "Subnet 'subnet-1' is in the 'failed' state, so it carries no working network. GPCN recorded no reason. Delete the subnet and create it again, or contact GPCN support."
	if got := diags.Warnings()[0].Detail(); got != want {
		t.Errorf("expected the detail %q, got %q", want, got)
	}

	// The platform can also record the reason as an empty string.
	empty := ""
	response.FailureReason = &empty

	diags = SubnetFailedWarning(response)
	if got := diags.Warnings()[0].Detail(); got != want {
		t.Errorf("expected the detail %q for an empty reason, got %q", want, got)
	}
}

// A ready subnet has nothing to warn about.
func TestSubnetFailedWarningSilentWhenReadyUnit(t *testing.T) {
	t.Parallel()

	if got := SubnetFailedWarning(unitTestApiSubnet()).WarningsCount(); got != 0 {
		t.Errorf("expected no warning for a ready subnet, got %d", got)
	}
}

// The NIC census is a live counter. Read and Update both write what the API
// reports, because the attribute carries no plan modifier and plans unknown.
func TestMapSubnetResponseToModelWritesFreshNicCountUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.AttachedNicCount = 7

	model := MapSubnetResponseToModel(response, ResourceModel{
		Name:             types.StringValue("subnet-a"),
		CIDR:             types.StringValue("10.50.1.0/24"),
		NsgID:            types.StringValue(unitTestNsgID),
		Description:      types.StringValue(""),
		AttachedNicCount: types.Int64Value(0),
	})

	if got := model.AttachedNicCount.ValueInt64(); got != 7 {
		t.Errorf("expected the fresh attached_nic_count 7, got %d", got)
	}
}

// Terraform reconciles a description in place, so Read shows the one GPCN
// holds. A null description reads as the empty string the schema defaults to.
func TestRefreshSubnetModelFromResponseUpdatesDescriptionUnit(t *testing.T) {
	t.Parallel()

	changed := "changed out of band"
	response := unitTestApiSubnet()
	response.Description = &changed

	model := RefreshSubnetModelFromResponse(response, ResourceModel{
		Description: types.StringValue("web tier"),
	})
	if got := model.Description.ValueString(); got != changed {
		t.Errorf("expected the refreshed description %q, got %q", changed, got)
	}

	response.Description = nil
	model = RefreshSubnetModelFromResponse(response, ResourceModel{
		Description: types.StringValue("web tier"),
	})
	if got := model.Description.ValueString(); got != "" {
		t.Errorf("expected a cleared description to read as the empty string, got %q", got)
	}
}

// The API never reports the prefix. The mapper reads it from the carved
// block's mask length when the model holds none. A configured prefix
// survives, because reconciling one replaces the subnet.
func TestMapSubnetResponseToModelFillsPrefixOnImportUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.CIDR = "10.50.1.0/26"

	imported := MapSubnetResponseToModel(response, ResourceModel{})
	if got := imported.Prefix.ValueInt64(); got != 26 {
		t.Errorf("expected the prefix 26 from the imported cidr, got %d", got)
	}

	created := MapSubnetResponseToModel(response, ResourceModel{
		CIDR:   types.StringValue("10.50.1.0/26"),
		Prefix: types.Int64Unknown(),
	})
	if got := created.Prefix.ValueInt64(); got != 26 {
		t.Errorf("expected a create to fill the unknown prefix with 26, got %d", got)
	}

	configured := MapSubnetResponseToModel(response, ResourceModel{Prefix: types.Int64Value(27)})
	if got := configured.Prefix.ValueInt64(); got != 27 {
		t.Errorf("expected a configured prefix to survive, got %d", got)
	}
}

// One create carries one correlation id. A poll that mints its own leaves the
// support log with no link between the create call and the job it waits for.
func TestCreateSubnetCarriesOneCorrelationIDUnit(t *testing.T) {
	t.Parallel()

	var createID, pollID string
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == client.JOBS_BASE_URL_V1 {
				if pollID == "" {
					pollID = r.Header.Get("x-Correlation-ID")
				}
				testutil.HandleJobResponse(w, "job-create", unitTestSubnetID, true)
				return
			}
			createID = r.Header.Get("x-Correlation-ID")
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data": map[string]any{
					"jobId":  "job-create",
					"subnet": map[string]any{"id": unitTestSubnetID, "cidr": "10.50.1.0/24", "nsgId": unitTestNsgID},
				},
			})
		},
	})

	ctx := client.WithCorrelationID(context.Background())
	issued, err := IssueCreateSubnet(gpcnClient, ctx, ResourceModel{
		VpcID:       types.StringValue(unitTestVpcID),
		Name:        types.StringValue("subnet-a"),
		Description: types.StringValue(""),
	})
	if err != nil {
		t.Fatalf("expected the create to be issued, got %v", err)
	}
	if err := PollCreateSubnet(gpcnClient, ctx, issued.JobID); err != nil {
		t.Fatalf("expected the carve poll to succeed, got %v", err)
	}

	if createID == "" {
		t.Fatal("the create carried no correlation id")
	}
	if pollID != createID {
		t.Errorf("poll correlation id = %q, want the create's %q", pollID, createID)
	}
}

// A 202 that names no subnet leaves the caller with nothing to write to state.
// The create must stop there, before the poll files a row Terraform cannot
// name.
func TestIssueCreateSubnetRejectsResponseWithoutIDUnit(t *testing.T) {
	t.Parallel()

	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data": map[string]any{
					"jobId":  "job-create",
					"subnet": map[string]any{"cidr": "10.50.1.0/24", "nsgId": unitTestNsgID},
				},
			})
		},
	})

	_, err := IssueCreateSubnet(gpcnClient, context.Background(), ResourceModel{
		VpcID:       types.StringValue(unitTestVpcID),
		Name:        types.StringValue("subnet-a"),
		Description: types.StringValue(""),
	})

	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if err.Error() != ErrDetailNoSubnetIDInCreate {
		t.Errorf("error = %q, want %q", err.Error(), ErrDetailNoSubnetIDInCreate)
	}
}
