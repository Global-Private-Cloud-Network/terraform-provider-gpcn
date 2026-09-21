package l2segments

import (
	"context"
	"net/http"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	unitTestSegmentID    = "6c0e4b1a-1d6e-4b6e-9d2b-000000000001"
	unitTestDatacenterID = "2f1c5f33-9a1e-4f2d-9d2b-000000000002"
	unitTestCreatedAt    = "2026-01-02T15:04:05Z"
	unitTestUpdatedAt    = "2026-03-04T05:06:07Z"
	unitTestCreatedRFC   = "Friday, 02-Jan-26 15:04:05 UTC"
	unitTestUpdatedRFC   = "Wednesday, 04-Mar-26 05:06:07 UTC"
)

func unitTestResponse() *readL2SegmentResponse {
	return &readL2SegmentResponse{
		Success: true,
		Data: l2SegmentData{
			ID:               unitTestSegmentID,
			Name:             "segment-a",
			Description:      nil,
			DatacenterId:     unitTestDatacenterID,
			DatacenterName:   nil,
			State:            "ready",
			Offering:         "plain",
			FailureReason:    nil,
			AttachedNicCount: 3,
			CreatedAt:        unitTestCreatedAt,
			UpdatedAt:        unitTestUpdatedAt,
		},
	}
}

// An import starts from a model whose every configured attribute is null, so the
// mapper is the only thing that can fill them.
func TestMapL2SegmentResponseToModelFillsNullAttributesUnit(t *testing.T) {
	t.Parallel()

	model := MapL2SegmentResponseToModel(unitTestResponse(), ResourceModel{})

	if got := model.ID.ValueString(); got != unitTestSegmentID {
		t.Errorf("id = %q, want %q", got, unitTestSegmentID)
	}
	if got := model.Name.ValueString(); got != "segment-a" {
		t.Errorf("name = %q, want %q", got, "segment-a")
	}
	if got := model.DatacenterId.ValueString(); got != unitTestDatacenterID {
		t.Errorf("datacenter_id = %q, want %q", got, unitTestDatacenterID)
	}
	if got := model.State.ValueString(); got != "ready" {
		t.Errorf("state = %q, want %q", got, "ready")
	}
	if got := model.Offering.ValueString(); got != "plain" {
		t.Errorf("offering = %q, want %q", got, "plain")
	}
	if got := model.AttachedNicCount.ValueInt64(); got != 3 {
		t.Errorf("attached_nic_count = %d, want 3", got)
	}
}

// A null description reads back as the empty string the schema defaults to. Every
// other nullable string stays null, because null is what the platform means.
func TestMapL2SegmentResponseToModelNormalizesNullsUnit(t *testing.T) {
	t.Parallel()

	model := MapL2SegmentResponseToModel(unitTestResponse(), ResourceModel{})

	if model.Description.IsNull() || model.Description.ValueString() != "" {
		t.Errorf("description = %#v, want an empty string", model.Description)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("failure_reason = %#v, want null", model.FailureReason)
	}
	if !model.DatacenterName.IsNull() {
		t.Errorf("datacenter_name = %#v, want null", model.DatacenterName)
	}
}

func TestMapL2SegmentResponseToModelFormatsTimestampsUnit(t *testing.T) {
	t.Parallel()

	model := MapL2SegmentResponseToModel(unitTestResponse(), ResourceModel{})

	if got := model.CreatedTime.ValueString(); got != unitTestCreatedRFC {
		t.Errorf("created_time = %q, want %q", got, unitTestCreatedRFC)
	}
	if got := model.LastUpdated.ValueString(); got != unitTestUpdatedRFC {
		t.Errorf("last_updated = %q, want %q", got, unitTestUpdatedRFC)
	}
}

// The mapper must not overwrite a configured value. Only the refresh may do that,
// and only for the two attributes the platform reconciles in place.
func TestMapL2SegmentResponseToModelKeepsConfiguredValuesUnit(t *testing.T) {
	t.Parallel()

	configured := ResourceModel{
		Name:         types.StringValue("configured-name"),
		DatacenterId: types.StringValue("configured-datacenter"),
		Description:  types.StringValue("configured-description"),
	}

	model := MapL2SegmentResponseToModel(unitTestResponse(), configured)

	if got := model.Name.ValueString(); got != "configured-name" {
		t.Errorf("name = %q, want the configured value", got)
	}
	if got := model.DatacenterId.ValueString(); got != "configured-datacenter" {
		t.Errorf("datacenter_id = %q, want the configured value", got)
	}
	if got := model.Description.ValueString(); got != "configured-description" {
		t.Errorf("description = %q, want the configured value", got)
	}
}

// The datacenter has no update verb, so a refreshed drift there would plan a
// replacement of a live carrier.
func TestRefreshL2SegmentModelFromResponseUnit(t *testing.T) {
	t.Parallel()

	response := unitTestResponse()
	described := "carried over"
	response.Data.Description = &described

	model := RefreshL2SegmentModelFromResponse(response, ResourceModel{
		Name:         types.StringValue("stale-name"),
		DatacenterId: types.StringValue("configured-datacenter"),
		Description:  types.StringValue("stale-description"),
	})

	if got := model.Name.ValueString(); got != "segment-a" {
		t.Errorf("name = %q, want the refreshed value", got)
	}
	if got := model.Description.ValueString(); got != described {
		t.Errorf("description = %q, want %q", got, described)
	}
	if got := model.DatacenterId.ValueString(); got != "configured-datacenter" {
		t.Errorf("datacenter_id = %q, want the configured value", got)
	}
}

func TestRefreshL2SegmentModelNormalizesNullDescriptionUnit(t *testing.T) {
	t.Parallel()

	model := RefreshL2SegmentModelFromResponse(unitTestResponse(), ResourceModel{
		Description: types.StringValue("stale-description"),
	})

	if model.Description.IsNull() || model.Description.ValueString() != "" {
		t.Errorf("description = %#v, want an empty string", model.Description)
	}
}

// The create body is strict at the wire, so a key the schema does not name is a
// 422 rather than an ignored field.
func TestCreateL2SegmentRequestBodyUnit(t *testing.T) {
	t.Parallel()

	body := createRequestBody(ResourceModel{
		Name:         types.StringValue("segment-a"),
		DatacenterId: types.StringValue(unitTestDatacenterID),
		Description:  types.StringValue("first"),
	})

	want := map[string]any{
		"name":         "segment-a",
		"datacenterId": unitTestDatacenterID,
		"description":  "first",
	}
	if len(body) != len(want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("body[%q] = %#v, want %#v", key, body[key], value)
		}
	}
}

func TestCreateL2SegmentRequestBodyOmitsEmptyDescriptionUnit(t *testing.T) {
	t.Parallel()

	body := createRequestBody(ResourceModel{
		Name:         types.StringValue("segment-a"),
		DatacenterId: types.StringValue(unitTestDatacenterID),
		Description:  types.StringValue(""),
	})

	if _, present := body["description"]; present {
		t.Errorf("body = %#v, want no description key", body)
	}
}

// The update schema takes only the keys that changed, and it refuses a body that
// names neither name nor description.
func TestUpdateL2SegmentRequestBodyUnit(t *testing.T) {
	t.Parallel()

	state := ResourceModel{
		Name:        types.StringValue("segment-a"),
		Description: types.StringValue("first"),
	}

	cases := []struct {
		name string
		plan ResourceModel
		want map[string]any
	}{
		{
			name: "rename only",
			plan: ResourceModel{Name: types.StringValue("segment-b"), Description: types.StringValue("first")},
			want: map[string]any{"name": "segment-b"},
		},
		{
			name: "description only",
			plan: ResourceModel{Name: types.StringValue("segment-a"), Description: types.StringValue("second")},
			want: map[string]any{"description": "second"},
		},
		{
			name: "both",
			plan: ResourceModel{Name: types.StringValue("segment-b"), Description: types.StringValue("second")},
			want: map[string]any{"name": "segment-b", "description": "second"},
		},
		{
			name: "nothing changed",
			plan: state,
			want: map[string]any{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := updateRequestBody(testCase.plan, state)
			if len(body) != len(testCase.want) {
				t.Fatalf("body = %#v, want %#v", body, testCase.want)
			}
			for key, value := range testCase.want {
				if body[key] != value {
					t.Errorf("body[%q] = %#v, want %#v", key, body[key], value)
				}
			}
		})
	}
}

// httpErrorFor builds the error an API refusal reaches the provider as. The real
// transport must stay in the stack, because it is what parses the envelope.
func httpErrorFor(t *testing.T, status int, body string) error {
	t.Helper()

	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		},
	})

	request, err := http.NewRequestWithContext(context.Background(), "DELETE", BASE_URL_V1+unitTestSegmentID, nil)
	if err != nil {
		t.Fatalf("failed to build the request: %v", err)
	}
	response, err := gpcnClient.DoWithRetry(request)
	if err == nil {
		_ = response.Body.Close()
		t.Fatalf("expected the refusal to surface as an error")
	}
	return err
}

// The in-use refusal is the one delete failure a user can act on, so the count it
// carries is repeated where the reader looks for it.
func TestL2SegmentDeleteRefusalDetailRendersAttachedCountUnit(t *testing.T) {
	t.Parallel()

	body := `{"success":false,"message":"Cannot delete an L2 segment with 2 attached network interface(s). Detach them or delete the VMs first.","error":{"code":"L2_SEGMENT_IN_USE","statusCode":409,"details":{"attachedNicCount":2}}}`
	detail := DeleteRefusalDetail(httpErrorFor(t, http.StatusConflict, body))

	want := "HTTP 409 (L2_SEGMENT_IN_USE): Cannot delete an L2 segment with 2 attached network interface(s). Detach them or delete the VMs first. (2 attached network interface(s))"
	if detail != want {
		t.Errorf("detail = %q, want %q", detail, want)
	}
}

// Every other refusal on the route carries the generic conflict code, so the
// provider forwards the platform sentence unchanged.
func TestL2SegmentDeleteRefusalDetailForwardsOtherConflictsUnit(t *testing.T) {
	t.Parallel()

	body := `{"success":false,"message":"Segment creation is in progress; wait for it to finish","error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`
	detail := DeleteRefusalDetail(httpErrorFor(t, http.StatusConflict, body))

	want := "HTTP 409 (Duplicate Resource): Segment creation is in progress; wait for it to finish"
	if detail != want {
		t.Errorf("detail = %q, want %q", detail, want)
	}
}

// A failed segment is live and tenant-visible, so Read keeps it in state and says
// why it failed. The plan harness cannot observe a warning, so the bytes are
// pinned here.
func TestL2SegmentFailedWarningUnit(t *testing.T) {
	t.Parallel()

	warning := FailedSegmentWarning(unitTestSegmentID, "Segment creation failed; delete the segment and try again")

	wantSummary := "L2 segment is in the failed state"
	wantDetail := "L2 segment " + unitTestSegmentID + " is in the failed state: Segment creation failed; delete the segment and try again. Destroy the segment and create it again."
	if got := warning.Summary(); got != wantSummary {
		t.Errorf("summary = %q, want %q", got, wantSummary)
	}
	if got := warning.Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}
	if warning.Severity() != diag.SeverityWarning {
		t.Errorf("severity = %v, want a warning", warning.Severity())
	}
}

// The platform can park a row failed with no reason recorded, and a sentence with
// an empty clause in it reads as a provider bug.
func TestL2SegmentFailedWarningWithoutReasonUnit(t *testing.T) {
	t.Parallel()

	warning := FailedSegmentWarning(unitTestSegmentID, "")

	wantDetail := "L2 segment " + unitTestSegmentID + " is in the failed state. Destroy the segment and create it again."
	if got := warning.Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}
}
