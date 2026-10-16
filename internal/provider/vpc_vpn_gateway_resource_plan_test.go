package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	vpnGatewayPlanTestVpcID     = "11111111-1111-4111-8111-111111111111"
	vpnGatewayPlanTestID        = "22222222-2222-4222-8222-222222222222"
	vpnGatewayPlanTestAddress   = "198.51.100.7"
	vpnGatewayPlanTestTimestamp = "2026-01-02T15:04:05Z"
	gpcnVpcVpnGatewayTest       = "gpcn_vpc_vpn_gateway.test"

	vpnGatewayPlanTestInUse   = "Remove the VPC's VPN connections before removing its VPN gateway."
	vpnGatewayPlanTestNoDC    = "This data center does not offer site-to-site VPN yet."
	vpnGatewayPlanTestJobFail = "VPN gateway creation failed; delete it and create it again"
	vpnGatewayPlanTestFail    = "The VPN gateway could not be created: its VPC was not ready. Remove it and create it again."
)

// vpnGatewayPlanTestServerState is the gateway the mock keeps between requests.
type vpnGatewayPlanTestServerState struct {
	mu            sync.Mutex
	exists        bool
	rowState      string
	failureReason string
	kind          string
	reservedBlock string
	// createdState is the state the gateway reads in once its job ends.
	createdState string
	// refuseCreate answers the POST with a 409 carrying this message.
	refuseCreate string
	// refuseDelete answers the next DELETE with a 409 carrying this message.
	refuseDelete string
	// failJob makes the job poll report a failed job.
	failJob bool
	// deleteGone answers the DELETE with a 404, as for a row removed meanwhile.
	deleteGone   bool
	createBodies []map[string]any
	deleteCount  int
}

func (s *vpnGatewayPlanTestServerState) row() map[string]any {
	var failureReason, reservedBlock, address any
	if s.failureReason != "" {
		failureReason = s.failureReason
	}
	if s.reservedBlock != "" {
		reservedBlock = s.reservedBlock
	}
	if s.rowState != "creating" {
		address = vpnGatewayPlanTestAddress
	}
	return map[string]any{
		"id":            vpnGatewayPlanTestID,
		"vpcId":         vpnGatewayPlanTestVpcID,
		"kind":          s.kind,
		"state":         s.rowState,
		"address":       address,
		"reservedBlock": reservedBlock,
		"failureReason": failureReason,
		"activeJobId":   nil,
		"createdAt":     vpnGatewayPlanTestTimestamp,
		"updatedAt":     vpnGatewayPlanTestTimestamp,
	}
}

func writeVpnGatewayPlanTestError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"success":false,"message":%q,"error":{"code":%q,"statusCode":%d,"details":null}}`, message, code, status)
}

func startVpnGatewayPlanMockServer(t *testing.T) (*httptest.Server, *vpnGatewayPlanTestServerState) {
	t.Helper()

	state := &vpnGatewayPlanTestServerState{kind: "router", createdState: "ready"}
	gatewayPath := "/v1/resource/vpcs/" + vpnGatewayPlanTestVpcID + "/vpn-gateway"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodGet && r.URL.Path == gatewayPath:
			state.mu.Lock()
			exists := state.exists
			row := state.row()
			state.mu.Unlock()
			if !exists {
				writeVpnGatewayPlanTestError(w, http.StatusNotFound, "VPN_GATEWAY_NOT_FOUND", "This VPC has no VPN gateway.")
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": row, "meta": nil})
		case r.Method == http.MethodPost && r.URL.Path == gatewayPath:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			refusal := state.refuseCreate
			if refusal == "" {
				state.createBodies = append(state.createBodies, body)
				state.exists = true
				state.rowState = state.createdState
				if state.createdState == "failed" {
					state.failureReason = vpnGatewayPlanTestFail
				}
			}
			state.mu.Unlock()
			if refusal != "" {
				writeVpnGatewayPlanTestError(w, http.StatusConflict, "Duplicate Resource", refusal)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data": map[string]any{
					"jobId": "job-create",
					"vpnGateway": map[string]any{
						"id": vpnGatewayPlanTestID, "vpcId": vpnGatewayPlanTestVpcID, "kind": "router",
						"state": "creating", "address": nil, "reservedBlock": nil, "failureReason": nil,
					},
				},
				"meta": nil,
			})
		case r.Method == http.MethodDelete && r.URL.Path == gatewayPath:
			state.mu.Lock()
			refusal := state.refuseDelete
			state.refuseDelete = ""
			gone := state.deleteGone
			if gone {
				state.exists = false
			}
			if refusal == "" && !gone {
				state.exists = false
				state.deleteCount++
			}
			state.mu.Unlock()
			if gone {
				writeVpnGatewayPlanTestError(w, http.StatusNotFound, "VPN_GATEWAY_NOT_FOUND", "This VPC has no VPN gateway.")
				return
			}
			if refusal != "" {
				writeVpnGatewayPlanTestError(w, http.StatusConflict, "VPN_GATEWAY_IN_USE", refusal)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"jobId": "job-delete", "vpnGateway": map[string]any{"id": vpnGatewayPlanTestID, "state": "removing"}},
				"meta":    nil,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			jobID := "job-1"
			if ids, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(ids) > 0 {
				jobID, _ = ids[0].(string)
			}
			state.mu.Lock()
			failJob := state.failJob
			state.mu.Unlock()
			if failJob {
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Data: client.JobStatusDataResponse{Jobs: []client.JobResponse{{
						JobID: jobID, ResourceId: vpnGatewayPlanTestID, HasFailed: true, ErrorMessage: vpnGatewayPlanTestJobFail,
					}}},
				})
				return
			}
			testutil.HandleJobResponse(w, jobID, vpnGatewayPlanTestID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

func vpnGatewayPlanTestConfig(host string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_vpn_gateway" "test" {
  vpc_id = %q
}
`, host, vpnGatewayPlanTestVpcID)
}

// vpnGatewayRefusal builds the pattern for a message Terraform prints inside a
// wrapped diagnostic. It lets any run of whitespace stand for a space.
func vpnGatewayRefusal(message string) *regexp.Regexp {
	return regexp.MustCompile(strings.ReplaceAll(regexp.QuoteMeta(message), " ", `\s+`))
}

func (s *vpnGatewayPlanTestServerState) set(update func(*vpnGatewayPlanTestServerState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s)
}

// The POST carries an empty object. The 202 says "creating" with a null
// address, and the state holds what the read after the job shows.
func TestVpcVpnGatewayResourceCreate(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	checkCreateBody := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if len(state.createBodies) != 1 {
			return fmt.Errorf("expected one create POST, got %d", len(state.createBodies))
		}
		if len(state.createBodies[0]) != 0 {
			return fmt.Errorf("expected an empty create body, got %v", state.createBodies[0])
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "id", vpnGatewayPlanTestID),
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "vpc_id", vpnGatewayPlanTestVpcID),
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "kind", "router"),
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "address", vpnGatewayPlanTestAddress),
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "state", "ready"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnGatewayTest, "reserved_block"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnGatewayTest, "failure_reason"),
					checkCreateBody,
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// A gateway that built badly reads back as failed. The apply stops with the
// failure reason, and the resource stays in state tainted, so the next apply
// replaces it even when the platform shows it healthy again.
func TestVpcVpnGatewayResourceCreateFailed(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	state.set(func(s *vpnGatewayPlanTestServerState) { s.createdState = "failed" })
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: vpnGatewayRefusal(vpnGatewayPlanTestFail),
			},
			{
				PreConfig: func() {
					state.set(func(s *vpnGatewayPlanTestServerState) {
						s.createdState = "ready"
						s.rowState = "ready"
						s.failureReason = ""
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnGatewayTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "state", "ready"),
			},
		},
	})
}

// The import ID is the VPC ID, because the gateway is the VPC's singleton.
func TestVpcVpnGatewayResourceImport(t *testing.T) {
	t.Parallel()
	server, _ := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      gpcnVpcVpnGatewayTest,
				ImportState:       true,
				ImportStateId:     vpnGatewayPlanTestVpcID,
				ImportStateVerify: true,
			},
		},
	})
}

// A refresh that shows the gateway failed plans a replacement. The API has no
// retry for a failed gateway.
func TestVpcVpnGatewayResourceFailedPlansReplace(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					state.set(func(s *vpnGatewayPlanTestServerState) {
						s.rowState = "failed"
						s.failureReason = vpnGatewayPlanTestFail
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnGatewayTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "state", "ready"),
			},
		},
	})
}

// A gateway with connections refuses the delete. The API's sentence reaches
// the operator unchanged.
func TestVpcVpnGatewayResourceDeleteInUse(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(*terraform.State) error {
					state.set(func(s *vpnGatewayPlanTestServerState) { s.refuseDelete = vpnGatewayPlanTestInUse })
					return nil
				},
			},
			{
				Config:      config,
				Destroy:     true,
				ExpectError: vpnGatewayRefusal(vpnGatewayPlanTestInUse),
			},
		},
	})
}

// A gateway removed outside Terraform answers 404. Read drops it, and the next
// plan creates it again.
func TestVpcVpnGatewayResourceGoneDropsState(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					state.set(func(s *vpnGatewayPlanTestServerState) { s.exists = false })
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnGatewayTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A data center without site-to-site VPN refuses the create with a 409 whose
// code is generic. Only the message tells the operator why.
func TestVpcVpnGatewayResourceDataCenterRefusal(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	state.set(func(s *vpnGatewayPlanTestServerState) { s.refuseCreate = vpnGatewayPlanTestNoDC })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpnGatewayPlanTestConfig(server.URL),
				ExpectError: vpnGatewayRefusal(vpnGatewayPlanTestNoDC),
			},
		},
	})
}

// A job that stops leaves the row behind. The create writes it to state with
// the error, so Terraform taints it and the next plan replaces it.
func TestVpcVpnGatewayResourceCreateJobFailedKeepsRow(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	state.set(func(s *vpnGatewayPlanTestServerState) { s.failJob = true; s.createdState = "failed" })
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: vpnGatewayRefusal(vpnGatewayPlanTestJobFail),
			},
			{
				PreConfig: func() {
					state.set(func(s *vpnGatewayPlanTestServerState) {
						s.failJob = false
						s.createdState = "ready"
						s.rowState = "ready"
						s.failureReason = ""
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnGatewayTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "state", "ready"),
			},
		},
	})
}

// A delete that answers 404 means the gateway is already gone. The destroy
// succeeds.
func TestVpcVpnGatewayResourceDeleteGone(t *testing.T) {
	t.Parallel()
	server, state := startVpnGatewayPlanMockServer(t)
	config := vpnGatewayPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(*terraform.State) error {
					state.set(func(s *vpnGatewayPlanTestServerState) { s.deleteGone = true })
					return nil
				},
			},
			{Config: config, Destroy: true},
		},
	})
}
