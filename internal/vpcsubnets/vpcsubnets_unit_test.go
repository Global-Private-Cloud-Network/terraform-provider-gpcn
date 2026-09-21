package vpcsubnets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

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

// A drifted CIDR plans a destroy of a subnet that can hold live interfaces, so
// the mapper must leave a value the configuration already carries.
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

// The update body carries the name and the description and no structural key,
// because the API refuses an unknown key and needs at least one of the two.
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

// A failed carve leaves a row Terraform reads without complaint, so the warning
// is the only place the operator learns the subnet carries no network.
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
	if got := warning.Summary(); got != WarnSummarySubnetFailed {
		t.Errorf("expected the summary %q, got %q", WarnSummarySubnetFailed, got)
	}
	want := fmt.Sprintf(WarnDetailSubnetFailed, unitTestSubnetID, reason)
	if got := warning.Detail(); got != want {
		t.Errorf("expected the detail %q, got %q", want, got)
	}
}

// A failed subnet with no reason still warrants the warning, and the sentence
// must stay readable where the reason would have been.
func TestSubnetFailedWarningWithoutReasonUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.State = StateFailed

	diags := SubnetFailedWarning(response)

	want := fmt.Sprintf(WarnDetailSubnetFailed, unitTestSubnetID, WarnDetailSubnetNoFailureReason)
	if got := diags.Warnings()[0].Detail(); got != want {
		t.Errorf("expected the detail %q, got %q", want, got)
	}
}

// A ready subnet has nothing to warn about.
func TestSubnetFailedWarningSilentWhenReadyUnit(t *testing.T) {
	t.Parallel()

	if got := SubnetFailedWarning(unitTestApiSubnet()).WarningsCount(); got != 0 {
		t.Errorf("expected no warning for a ready subnet, got %d", got)
	}
}

// The plan pins attached_nic_count to the prior state, so an Update that wrote
// a fresher census would end the apply with an inconsistent result. The mapper
// must leave a count the caller already holds.
func TestMapSubnetResponseToModelKeepsPlannedNicCountUnit(t *testing.T) {
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

	if got := model.AttachedNicCount.ValueInt64(); got != 0 {
		t.Errorf("expected the planned attached_nic_count 0 to survive the mapper, got %d", got)
	}
}

// Read is where a fresher census belongs: reconciling a count destroys nothing.
func TestRefreshSubnetModelFromResponseUpdatesNicCountUnit(t *testing.T) {
	t.Parallel()

	response := unitTestApiSubnet()
	response.AttachedNicCount = 7

	model := RefreshSubnetModelFromResponse(response, ResourceModel{
		AttachedNicCount: types.Int64Value(0),
	})

	if got := model.AttachedNicCount.ValueInt64(); got != 7 {
		t.Errorf("expected the refreshed attached_nic_count 7, got %d", got)
	}
}
