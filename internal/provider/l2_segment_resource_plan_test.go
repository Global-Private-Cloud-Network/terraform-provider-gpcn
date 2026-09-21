package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/l2segments"
	"terraform-provider-gpcn/internal/testutil"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gpcnL2SegmentTest      = "gpcn_l2_segment.test"
	l2PlanTestSegmentID    = "seg-1"
	l2PlanTestDatacenterID = "dc-1"
	l2PlanTestOtherDCID    = "dc-2"
	l2PlanTestTimestamp    = "2026-01-02T15:04:05Z"
	l2PlanTestRFC850       = "Friday, 02-Jan-26 15:04:05 UTC"
	l2PlanTestName         = "segment-plan-a"
	l2PlanTestRenamedName  = "segment-plan-b"
	l2PlanTestFailedState  = "failed"
	l2PlanTestFailedReason = "Segment creation failed; delete the segment and try again"
)

// l2PlanTestDetailBody answers with the detail projection. The description is the
// one nullable field the provider normalizes, so the fixture leaves it null.
func l2PlanTestDetailBody(name string, description any, nicCount int64) map[string]any {
	return l2PlanTestDetailBodyInState(name, description, "ready", nil, nicCount)
}

func l2PlanTestDetailBodyInState(name string, description any, segmentState string, failureReason any, nicCount int64) map[string]any {
	return map[string]any{
		"success": true,
		"message": "",
		"data": map[string]any{
			"id":               l2PlanTestSegmentID,
			"name":             name,
			"description":      description,
			"datacenterId":     l2PlanTestDatacenterID,
			"datacenterName":   nil,
			"resourceGroupId":  nil,
			"state":            segmentState,
			"offering":         "plain",
			"failureReason":    failureReason,
			"attachedNicCount": nicCount,
			"createdAt":        l2PlanTestTimestamp,
			"updatedAt":        l2PlanTestTimestamp,
		},
		"meta": nil,
	}
}

// l2PlanTestInUseBody is the refusal a delete meets while interfaces sit on the
// segment. It is the only delete refusal that carries a typed code and a count.
func l2PlanTestInUseBody() map[string]any {
	return map[string]any{
		"success": false,
		"message": "Cannot delete an L2 segment with 2 attached network interface(s). Detach them or delete the VMs first.",
		"error": map[string]any{
			"code":       "L2_SEGMENT_IN_USE",
			"statusCode": 409,
			"details":    map[string]any{"attachedNicCount": 2},
		},
	}
}

// l2PlanTestServer holds what the mock remembers between requests.
type l2PlanTestServer struct {
	mu sync.Mutex
	// name and description are what the last write left on the segment.
	name        string
	description any
	// updateBody is the body of the last rename or description edit.
	updateBody map[string]any
	// segmentState and failureReason are what the platform parked on the row.
	segmentState  string
	failureReason any
	// nicCount is the live interface count the platform reports. attachedOnUpdate
	// is an attach that lands during the apply, after the plan is made.
	nicCount         int64
	attachedOnUpdate *int64
	// refusalsLeft counts the deletes that answer with the in-use refusal. The
	// test framework destroys once more after a failed step. That destroy must
	// succeed, or the run leaves the case red for the wrong reason.
	refusalsLeft int
}

func (s *l2PlanTestServer) lastUpdateBody() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateBody
}

func (s *l2PlanTestServer) refuseNextDelete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusalsLeft = 1
}

// parkFailed is what the platform does to a segment whose workflow did not finish.
func (s *l2PlanTestServer) parkFailed(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.segmentState = l2PlanTestFailedState
	s.failureReason = reason
}

// attachNicDuringUpdate moves the count between the plan and the apply. Only the
// update answer carries the new count, so the plan does not see it.
func (s *l2PlanTestServer) attachNicDuringUpdate(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attachedOnUpdate = &count
}

func (s *l2PlanTestServer) setName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// startL2SegmentPlanMockServer serves every route the resource drives.
func startL2SegmentPlanMockServer(t *testing.T) (*httptest.Server, *l2PlanTestServer) {
	t.Helper()

	state := &l2PlanTestServer{segmentState: "ready"}
	segmentPath := "/v1/resource/l2-segments/" + l2PlanTestSegmentID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/l2-segments/":
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.name, _ = body["name"].(string)
			if description, present := body["description"]; present {
				state.description = description
			}
			state.mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-create", "Operation initiated successfully")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-create", l2PlanTestSegmentID, true)
		case r.Method == http.MethodGet && r.URL.Path == segmentPath:
			state.mu.Lock()
			name, description := state.name, state.description
			segmentState, failureReason := state.segmentState, state.failureReason
			nicCount := state.nicCount
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, l2PlanTestDetailBodyInState(name, description, segmentState, failureReason, nicCount))
		case r.Method == http.MethodPut && r.URL.Path == segmentPath:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.updateBody = body
			if name, present := body["name"].(string); present {
				state.name = name
			}
			if description, present := body["description"]; present {
				state.description = description
			}
			if state.attachedOnUpdate != nil {
				state.nicCount = *state.attachedOnUpdate
				state.attachedOnUpdate = nil
			}
			name, description, nicCount := state.name, state.description, state.nicCount
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, l2PlanTestDetailBody(name, description, nicCount))
		case r.Method == http.MethodDelete && r.URL.Path == segmentPath:
			state.mu.Lock()
			refuse := state.refusalsLeft > 0
			if refuse {
				state.refusalsLeft--
			}
			state.mu.Unlock()
			if refuse {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				testutil.WriteJSONResponse(w, l2PlanTestInUseBody())
				return
			}
			testutil.HandleCreateJobResponse(w, "job-delete", "Operation initiated successfully")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

func l2PlanTestConfig(host, name string) string {
	return l2PlanTestConfigInDatacenter(host, name, l2PlanTestDatacenterID)
}

func l2PlanTestConfigInDatacenter(host, name, datacenterID string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_l2_segment" "test" {
  name          = %q
  datacenter_id = %q
}
`, host, name, datacenterID)
}

// A job creates the segment and names it only once the job completes. The case
// then reads the detail, renames the segment in place and destroys it.
func TestL2SegmentResourcePlanCreateRenameDestroy(t *testing.T) {
	t.Parallel()
	server, state := startL2SegmentPlanMockServer(t)

	checkUpdateBody := func(*terraform.State) error {
		body := state.lastUpdateBody()
		if body == nil {
			return fmt.Errorf("expected a rename PUT, got none")
		}
		if got, _ := body["name"].(string); got != l2PlanTestRenamedName {
			return fmt.Errorf("expected update body name %q, got %q", l2PlanTestRenamedName, got)
		}
		if _, present := body["description"]; present {
			return fmt.Errorf("expected no description key in the update body, got %#v", body)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: l2PlanTestConfig(server.URL, l2PlanTestName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "id", l2PlanTestSegmentID),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "name", l2PlanTestName),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "datacenter_id", l2PlanTestDatacenterID),
					// The platform answered null, and the schema defaults to "".
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "description", ""),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "state", "ready"),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "offering", "plain"),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "attached_nic_count", "0"),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "created_time", l2PlanTestRFC850),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "last_updated", l2PlanTestRFC850),
					// Every other nullable string stays null.
					resource.TestCheckNoResourceAttr(gpcnL2SegmentTest, "failure_reason"),
					resource.TestCheckNoResourceAttr(gpcnL2SegmentTest, "datacenter_name"),
				),
			},
			{
				Config: l2PlanTestConfig(server.URL, l2PlanTestRenamedName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "name", l2PlanTestRenamedName),
					checkUpdateBody,
				),
			},
		},
	})
}

// GPCN trims a padded name before it stores it. The configuration never
// settles, so the plan refuses the value.
func TestL2SegmentResourcePlanRefusesPaddedName(t *testing.T) {
	t.Parallel()
	server, _ := startL2SegmentPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: l2PlanTestConfig(server.URL, " seg"),
				// Terraform wraps a diagnostic, so the pattern tolerates a line
				// break inside the sentence.
				ExpectError: regexp.MustCompile(`(?s)Invalid L2 segment name.*name must not start or end with\s+whitespace`),
			},
		},
	})
}

// A NIC can land between the plan and the apply. The attribute therefore plans
// unknown, and the apply writes what the API reports.
func TestL2SegmentResourcePlanAcceptsNicCountMovedDuringApply(t *testing.T) {
	t.Parallel()
	server, state := startL2SegmentPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: l2PlanTestConfig(server.URL, l2PlanTestName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "attached_nic_count", "0"),
				),
			},
			{
				PreConfig: func() { state.attachNicDuringUpdate(2) },
				Config:    l2PlanTestConfig(server.URL, l2PlanTestRenamedName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "attached_nic_count", "2"),
				),
			},
		},
	})
}

// A rename the platform made out of band reconciles in place, because the update
// verb accepts a name.
func TestL2SegmentResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	server, state := startL2SegmentPlanMockServer(t)

	config := l2PlanTestConfig(server.URL, l2PlanTestName)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				PreConfig: func() { state.setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "name", l2PlanTestName),
				),
			},
		},
	})
}

// The in-use refusal names the interfaces the user must detach first. The count
// reaches the reader at the end of the sentence, where a number is looked for.
func TestL2SegmentResourcePlanRefusesDeleteInUse(t *testing.T) {
	t.Parallel()
	server, state := startL2SegmentPlanMockServer(t)

	config := l2PlanTestConfig(server.URL, l2PlanTestName)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				PreConfig: func() { state.refuseNextDelete() },
				Config:    config,
				Destroy:   true,
				// Terraform wraps a diagnostic, so the pattern tolerates a line
				// break inside the sentence.
				ExpectError: regexp.MustCompile(`(?s)L2_SEGMENT_IN_USE.*\(2 attached network\s+interface\(s\)\)`),
			},
		},
	})
}

// A failed segment is live and tenant-visible. Read keeps it in state and shows
// the reason, so the user can destroy it. Removing it would plan a create that
// leaves the parked row behind.
func TestL2SegmentResourcePlanKeepsFailedSegmentInState(t *testing.T) {
	t.Parallel()
	server, state := startL2SegmentPlanMockServer(t)

	config := l2PlanTestConfig(server.URL, l2PlanTestName)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				PreConfig: func() { state.parkFailed(l2PlanTestFailedReason) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "state", l2PlanTestFailedState),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "failure_reason", l2PlanTestFailedReason),
				),
			},
		},
	})
}

// The plan harness carries no warning assertion, so Read is driven directly. A
// test on the constructor alone leaves the call site unguarded.
func TestL2SegmentReadWarnsOnFailedSegmentUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	readInState := func(segmentState string, failureReason any) fwresource.ReadResponse {
		t.Helper()

		_, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
			T: t,
			Handler: func(w http.ResponseWriter, _ *http.Request) {
				testutil.WriteJSONResponse(w, l2PlanTestDetailBodyInState(
					l2PlanTestName, nil, segmentState, failureReason, 0,
				))
			},
		})

		segmentResource := &l2SegmentResource{client: gpcnClient}

		var schemaResponse fwresource.SchemaResponse
		segmentResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

		priorState := tfsdk.State{Schema: schemaResponse.Schema}
		diags := priorState.Set(ctx, l2segments.ResourceModel{
			ID:               types.StringValue(l2PlanTestSegmentID),
			Name:             types.StringValue(l2PlanTestName),
			DatacenterId:     types.StringValue(l2PlanTestDatacenterID),
			Description:      types.StringValue(""),
			State:            types.StringValue("ready"),
			Offering:         types.StringValue("plain"),
			FailureReason:    types.StringNull(),
			DatacenterName:   types.StringNull(),
			AttachedNicCount: types.Int64Value(0),
			CreatedTime:      types.StringValue(l2PlanTestRFC850),
			LastUpdated:      types.StringValue(l2PlanTestRFC850),
		})
		if diags.HasError() {
			t.Fatalf("failed to build the prior state: %v", diags)
		}

		readResponse := fwresource.ReadResponse{
			State: tfsdk.State{Schema: schemaResponse.Schema, Raw: priorState.Raw},
		}
		segmentResource.Read(ctx, fwresource.ReadRequest{State: priorState}, &readResponse)

		if readResponse.Diagnostics.HasError() {
			t.Fatalf("Read reported errors: %v", readResponse.Diagnostics.Errors())
		}
		return readResponse
	}

	readResponse := readInState(l2PlanTestFailedState, l2PlanTestFailedReason)

	warnings := readResponse.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != l2segments.WarnSummaryL2SegmentFailed {
		t.Errorf("summary = %q, want %q", got, l2segments.WarnSummaryL2SegmentFailed)
	}
	wantDetail := fmt.Sprintf(l2segments.WarnDetailL2SegmentFailed, l2PlanTestSegmentID, l2PlanTestFailedReason)
	if got := warnings[0].Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}

	// A ready segment must stay silent. Without this drive an unconditional
	// warning passes every assertion above.
	readyResponse := readInState("ready", nil)
	if quiet := readyResponse.Diagnostics.Warnings(); len(quiet) != 0 {
		t.Errorf("warnings on a ready segment = %v, want none", quiet)
	}

	// The platform can park a segment with no reason recorded. A guard on the
	// reason instead of the state reads such a row in silence.
	noReasonResponse := readInState(l2PlanTestFailedState, nil)
	noReasonWarnings := noReasonResponse.Diagnostics.Warnings()
	if len(noReasonWarnings) != 1 {
		t.Fatalf("warnings on a failed segment with no reason = %v, want exactly one", noReasonWarnings)
	}
	wantNoReasonDetail := fmt.Sprintf(l2segments.WarnDetailL2SegmentFailedNoReason, l2PlanTestSegmentID)
	if got := noReasonWarnings[0].Detail(); got != wantNoReasonDetail {
		t.Errorf("detail = %q, want %q", got, wantNoReasonDetail)
	}
}

// A segment is pinned to one datacenter. The platform has no verb that moves it,
// so a new datacenter is a new segment.
func TestL2SegmentResourcePlanReplacesOnDatacenterChange(t *testing.T) {
	t.Parallel()
	server, _ := startL2SegmentPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: l2PlanTestConfigInDatacenter(server.URL, l2PlanTestName, l2PlanTestDatacenterID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				Config: l2PlanTestConfigInDatacenter(server.URL, l2PlanTestName, l2PlanTestOtherDCID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "datacenter_id", l2PlanTestOtherDCID),
				),
			},
		},
	})
}
