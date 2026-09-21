package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The provider forwards the platform's own refusal sentence. The test pins
// those bytes, not a summary the provider writes. Terraform wraps a
// diagnostic to the terminal width, so the pattern accepts a break at any space.
var regexpPublicIpReleaseInProgress = regexp.MustCompile(`Public\s+IP\s+release\s+is\s+in\s+progress;\s+wait\s+for\s+it\s+to\s+finish`)

// A failed acquire names the address it left behind. The id is what the
// operator needs to find the row.
var regexpPublicIpAcquiredButJobFailed = regexp.MustCompile(`public\s+IP\s+` + vpcPublicIpPlanTestID + `\s+was\s+acquired\s+and\s+is\s+in\s+state`)

const (
	vpcPublicIpPlanTestVpcID     = "11111111-1111-1111-1111-111111111111"
	vpcPublicIpPlanTestID        = "22222222-2222-2222-2222-222222222222"
	vpcPublicIpPlanTestNicID     = "33333333-3333-3333-3333-333333333333"
	vpcPublicIpPlanTestOtherNic  = "55555555-5555-5555-5555-555555555555"
	vpcPublicIpPlanTestOtherVpc  = "66666666-6666-6666-6666-666666666666"
	vpcPublicIpPlanTestVmID      = "44444444-4444-4444-4444-444444444444"
	vpcPublicIpPlanTestAddress   = "203.0.113.10"
	vpcPublicIpPlanTestTimestamp = "2026-01-02T15:04:05Z"
	// The acquire job carries its own id, so one test can fail it alone.
	vpcPublicIpPlanTestAcquireJob = "job-acquire"
)

// publicIpPlanTestRow is the mock's copy of the one address row. The tests
// move it through the states the platform writes. The provider then reads
// what a real acquisition, attach and release show.
type publicIpPlanTestRow struct {
	mu        sync.Mutex
	state     string
	address   *string
	machineID *string
	released  bool
	// refuseFirstRelease makes the first DELETE answer the 409 the platform
	// sends while a release is already running.
	refuseFirstRelease bool
	// acquireJobFailed makes the acquire job report a failure. The row still
	// exists, because the API inserts it before it dispatches the job.
	acquireJobFailed bool
	releaseCalls     int
	detachCalls      int
	attachStatus     int
	attachMessage    string
	lastAttachBody   map[string]any
}

func (row *publicIpPlanTestRow) body() map[string]any {
	row.mu.Lock()
	defer row.mu.Unlock()
	return map[string]any{
		"id":                 vpcPublicIpPlanTestID,
		"ipAddress":          row.address,
		"state":              row.state,
		"failureReason":      nil,
		"virtualMachineId":   row.machineID,
		"virtualMachineName": nil,
		"held":               row.machineID == nil,
		"activeJobId":        nil,
		"createdAt":          vpcPublicIpPlanTestTimestamp,
		"updatedAt":          vpcPublicIpPlanTestTimestamp,
	}
}

func (row *publicIpPlanTestRow) becomeReady() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.state = "ready"
	address := vpcPublicIpPlanTestAddress
	row.address = &address
}

func (row *publicIpPlanTestRow) release() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.released = true
}

// reacquire puts the row back in the listing. A second acquire does that
// after the first address was released.
func (row *publicIpPlanTestRow) reacquire() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.released = false
	row.state = "acquiring"
	row.address = nil
	row.machineID = nil
}

// detach clears the binding the way a detach outside Terraform does.
func (row *publicIpPlanTestRow) detach() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.machineID = nil
}

// refuseAttach makes every attach answer the given refusal.
func (row *publicIpPlanTestRow) refuseAttach(status int, message string) {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.attachStatus = status
	row.attachMessage = message
}

func (row *publicIpPlanTestRow) isReleased() bool {
	row.mu.Lock()
	defer row.mu.Unlock()
	return row.released
}

// refuseFirstReleaseOnce arms the refusal under the lock. The mock server reads
// the field from its own goroutine.
func (row *publicIpPlanTestRow) refuseFirstReleaseOnce() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.refuseFirstRelease = true
}

// failAcquireJob arms the failure under the lock. The mock server reads the
// field from its own goroutine.
func (row *publicIpPlanTestRow) failAcquireJob() {
	row.mu.Lock()
	defer row.mu.Unlock()
	row.acquireJobFailed = true
}

func (row *publicIpPlanTestRow) acquireJobFails() bool {
	row.mu.Lock()
	defer row.mu.Unlock()
	return row.acquireJobFailed
}

func (row *publicIpPlanTestRow) releaseCallCount() int {
	row.mu.Lock()
	defer row.mu.Unlock()
	return row.releaseCalls
}

func (row *publicIpPlanTestRow) detachCallCount() int {
	row.mu.Lock()
	defer row.mu.Unlock()
	return row.detachCalls
}

// startVPCPublicIpPlanMockServer serves the four address routes plus the auth
// check and the job poll. The returned row is the state the mock answers with.
func startVPCPublicIpPlanMockServer(t *testing.T) (*httptest.Server, *publicIpPlanTestRow) {
	t.Helper()

	row := &publicIpPlanTestRow{state: "acquiring", attachStatus: http.StatusAccepted}

	listPath := "/v1/resource/vpcs/" + vpcPublicIpPlanTestVpcID + "/public-ips"
	addressPath := listPath + "/" + vpcPublicIpPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			publicIpPlanTestHandleJob(w, r, row)
		case r.Method == http.MethodPost && r.URL.Path == listPath:
			row.reacquire()
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"publicIpId": vpcPublicIpPlanTestID, "jobId": vpcPublicIpPlanTestAcquireJob},
			})
		case r.Method == http.MethodGet && r.URL.Path == listPath:
			rows := []map[string]any{}
			if !row.isReleased() {
				rows = append(rows, row.body())
			}
			testutil.WriteJSONResponse(w, publicIpPlanTestListBody(rows))
		case r.Method == http.MethodPost && r.URL.Path == addressPath+"/attach":
			publicIpPlanTestHandleAttach(w, r, row)
		case r.Method == http.MethodPost && r.URL.Path == addressPath+"/detach":
			row.mu.Lock()
			row.machineID = nil
			row.detachCalls++
			row.mu.Unlock()
			publicIpPlanTestAcceptJob(w)
		case r.Method == http.MethodDelete && r.URL.Path == addressPath:
			publicIpPlanTestHandleRelease(w, row)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, row
}

// publicIpPlanTestHandleJob answers the poll for the job the request names.
func publicIpPlanTestHandleJob(w http.ResponseWriter, r *http.Request, row *publicIpPlanTestRow) {
	jobID := "job-1"
	if ids, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(ids) > 0 {
		jobID, _ = ids[0].(string)
	}

	if jobID == vpcPublicIpPlanTestAcquireJob && row.acquireJobFails() {
		testutil.WriteJSONResponse(w, map[string]any{
			"success": true,
			"message": "Job status retrieved",
			"data": map[string]any{"jobs": []map[string]any{{
				"jobId":        jobID,
				"isCompleted":  false,
				"isTerminal":   true,
				"hasFailed":    true,
				"errorMessage": "No addresses are free in this region",
			}}},
		})
		return
	}

	testutil.HandleJobResponse(w, jobID, vpcPublicIpPlanTestID, true)
}

func publicIpPlanTestHandleAttach(w http.ResponseWriter, r *http.Request, row *publicIpPlanTestRow) {
	body := testutil.ReadRequestBody(r)
	row.mu.Lock()
	row.lastAttachBody = body
	status := row.attachStatus
	message := row.attachMessage
	if status == http.StatusAccepted {
		machineID := vpcPublicIpPlanTestVmID
		row.machineID = &machineID
	}
	row.mu.Unlock()

	if status != http.StatusAccepted {
		w.WriteHeader(status)
		testutil.WriteJSONResponse(w, map[string]any{
			"success": false,
			"message": message,
			"error":   map[string]any{"code": "Update Failed", "statusCode": status, "details": nil},
		})
		return
	}
	publicIpPlanTestAcceptJob(w)
}

func publicIpPlanTestHandleRelease(w http.ResponseWriter, row *publicIpPlanTestRow) {
	row.mu.Lock()
	row.releaseCalls++
	refuse := row.refuseFirstRelease && row.releaseCalls == 1
	row.mu.Unlock()

	if refuse {
		w.WriteHeader(http.StatusConflict)
		testutil.WriteJSONResponse(w, map[string]any{
			"success": false,
			"message": "Public IP release is in progress; wait for it to finish",
			"error":   map[string]any{"code": "DUPLICATE_RESOURCE", "statusCode": 409, "details": nil},
		})
		return
	}

	row.release()
	publicIpPlanTestAcceptJob(w)
}

func publicIpPlanTestAcceptJob(w http.ResponseWriter) {
	testutil.WriteJSONResponse(w, map[string]any{
		"success": true,
		"message": "Operation initiated successfully",
		"data":    map[string]any{"jobId": "job-1"},
	})
}

func publicIpPlanTestListBody(rows []map[string]any) map[string]any {
	return map[string]any{
		"success": true,
		"message": "",
		"data":    rows,
		"meta": map[string]any{
			"total":           len(rows),
			"page":            1,
			"pageSize":        100,
			"totalPages":      1,
			"hasNextPage":     false,
			"hasPreviousPage": false,
		},
	}
}

func vpcPublicIpPlanTestConfig(host string) string {
	return vpcPublicIpPlanTestConfigForVpc(host, vpcPublicIpPlanTestVpcID)
}

func vpcPublicIpPlanTestConfigForVpc(host, vpcID string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_public_ip" "test" {
  vpc_id = %q
}
`, host, vpcID)
}

// An address belongs to the VPC that acquired it. A new vpc_id must therefore
// plan a replacement, not an in-place update.
func TestVPCPublicIpResourcePlanReplacesOnVpcIdChange(t *testing.T) {
	t.Parallel()
	server, _ := startVPCPublicIpPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPublicIpPlanTestConfig(server.URL),
			},
			{
				Config:             vpcPublicIpPlanTestConfigForVpc(server.URL, vpcPublicIpPlanTestOtherVpc),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpTest, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

// An address the platform has not allocated yet carries a null address. The
// state must show that null rather than an empty string. A later read must
// pick the address up without planning a change.
func TestVPCPublicIpResourcePlanAcquireReadAndRelease(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	config := vpcPublicIpPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "id", vpcPublicIpPlanTestID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "vpc_id", vpcPublicIpPlanTestVpcID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "state", "acquiring"),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "held", "true"),
					resource.TestCheckNoResourceAttr(gpcnVPCPublicIpTest, "ip_address"),
					resource.TestCheckNoResourceAttr(gpcnVPCPublicIpTest, "virtual_machine_id"),
					resource.TestCheckNoResourceAttr(gpcnVPCPublicIpTest, "failure_reason"),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "created_time", "Friday, 02-Jan-26 15:04:05 UTC"),
				),
			},
			{
				PreConfig: row.becomeReady,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "state", "ready"),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "ip_address", vpcPublicIpPlanTestAddress),
				),
			},
		},
	})
}

// An address released outside Terraform leaves the listing. The read must drop
// the resource so the next plan acquires a new address.
func TestVPCPublicIpResourcePlanRemovesReleasedAddressFromState(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	config := vpcPublicIpPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: row.release,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// The platform refuses a release it is already running. The provider forwards
// that refusal with the platform's own sentence.
func TestVPCPublicIpResourcePlanSurfacesReleaseRefusal(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	config := vpcPublicIpPlanTestConfig(server.URL)
	row.refuseFirstReleaseOnce()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				Config:      config,
				Destroy:     true,
				ExpectError: regexpPublicIpReleaseInProgress,
			},
		},
	})
}

// The platform inserts the address row before it dispatches the acquire job.
// A failed job must therefore leave the id in state. The destroy then releases
// the address instead of leaving a billable row outside Terraform.
func TestVPCPublicIpResourcePlanKeepsTheIdWhenTheAcquireJobFails(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.failAcquireJob()

	config := vpcPublicIpPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpPublicIpAcquiredButJobFailed,
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkPublicIpDestroyReleasedTheAddress(row),
			},
		},
	})
}

// checkPublicIpDestroyReleasedTheAddress proves the failed create wrote the id
// to state. The mock routes the release by that id, so a create that kept the
// id to itself leaves the destroy with nothing to release.
func checkPublicIpDestroyReleasedTheAddress(row *publicIpPlanTestRow) func(*terraform.State) error {
	return func(*terraform.State) error {
		if row.releaseCallCount() == 0 {
			return fmt.Errorf("expected the destroy to release the acquired address, got no release request")
		}
		if !row.isReleased() {
			return fmt.Errorf("expected the address to be released, but the mock still lists it")
		}
		return nil
	}
}
