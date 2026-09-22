package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gpcnVpcTest             = "gpcn_vpc.test"
	vpcPlanTestID           = "vpc-1"
	vpcPlanTestDatacenterID = "dc-1"
	vpcPlanTestCidr         = "10.50.0.0/16"
	vpcPlanTestTimestamp    = "2026-01-02T15:04:05Z"
	vpcPlanTestCreateJobID  = "job-create"
	vpcPlanTestDeleteJobID  = "job-delete"
)

const vpcOverlapRefusalBody = `{"success":false,` +
	`"message":"CIDR overlaps existing VPC(s): \"web\" (10.50.0.0/16). Overlapping VPCs can never be connected to each other. Re-submit with acknowledgeOverlap: true to proceed.",` +
	`"error":{"code":"VPC_CIDR_OVERLAP_UNCONFIRMED","statusCode":409,` +
	`"details":{"overlapping":[{"id":"vpc-2","name":"web","cidr":"10.50.0.0/16"}],"hiddenOverlapCount":0}}}`

const vpcNotEmptyRefusalBody = `{"success":false,` +
	`"message":"Cannot delete VPC with 1 subnet(s). Delete subnets and network security groups and release public IPs first.",` +
	`"error":{"code":"VPC_NOT_EMPTY","statusCode":409,` +
	`"details":{"blockers":{"subnets":1,"publicIps":0,"nsgs":0},"inFlight":{"subnets":0,"publicIps":0,"nsgs":0}}}}`

// The API refuses an update body that carries no updatable key.
const vpcEmptyUpdateRefusalBody = `{"success":false,` +
	`"message":"Invalid Parameters: At least one of name, description or resourceGroupId must be provided",` +
	`"error":{"code":"Validation Error","statusCode":422,` +
	`"details":{"issues":[{"path":"","message":"At least one of name, description or resourceGroupId must be provided"}]}}}`

const vpcNotFoundBody = `{"success":false,"message":"VPC not found",` +
	`"error":{"code":"Resource Not Found","statusCode":404,"details":null}}`

// A VPC already being torn down refuses a second claim.
const vpcTearingDownRefusalBody = `{"success":false,` +
	`"message":"The VPC is not active (status: deleting); this operation needs an active VPC.",` +
	`"error":{"code":"VPC_NOT_ACTIVE","statusCode":409,"details":null}}`

// The marker a teardown writes when its dispatch dies before the engine.
const vpcPlanTestParkedReason = "VPC removal could not be started; retry the delete"

// The text a job carries when its worker dies before any terminal stage.
const vpcPlanTestAbandonedJobError = "Job was dispatched but never reached a terminal stage " +
	"(worker crash or restart) \u2014 reclaimed by the stale-job sweep."

// vpcMock serves the VPC endpoints an apply walks. It keeps the name the last
// create or update sent. The read after an apply then agrees with the
// configuration, and the refresh plan stays empty.
type vpcMock struct {
	url string

	mutex          sync.Mutex
	name           string
	description    string
	status         string
	cidr           string
	nameservers    []string
	createRefusal  string
	deleteRefusals int
	deleteNotFound bool
	getNotFound    bool
	createBody     map[string]any
	createBodies   []map[string]any
	requests       []string
	// tearingDownDeletes counts the deletes that answer the refusal a live
	// teardown raises. The first of them parks the row in deleting.
	tearingDownDeletes int
	// parkedTeardownDeletes counts the deletes that lose the same race to a
	// teardown whose dispatch dies. The row then carries a failure reason.
	parkedTeardownDeletes int
	failureReason         any
	tearingDown           bool
	// deletingGets is how many reads still answer deleting. The row answers 404
	// after them, which is the end of the teardown.
	deletingGets   int
	getsWhileGoing int
	// deleteJobFailures counts the teardown jobs that answer a failure. A row
	// such a job leaves behind never reaches 404.
	deleteJobFailures int
}

// vpcMockRefusals seeds the answers the mock refuses with. A test states them
// before the server starts, because the handler goroutine reads them.
type vpcMockRefusals struct {
	create                string
	deletes               int
	deleteNotFound        bool
	tearingDownDeletes    int
	parkedTeardownDeletes int
	deleteJobFailures     int
}

func startVpcPlanMockServer(t *testing.T, refusals vpcMockRefusals) *vpcMock {
	t.Helper()

	mock := &vpcMock{
		cidr:                  vpcPlanTestCidr,
		status:                "active",
		createRefusal:         refusals.create,
		deleteRefusals:        refusals.deletes,
		deleteNotFound:        refusals.deleteNotFound,
		tearingDownDeletes:    refusals.tearingDownDeletes,
		parkedTeardownDeletes: refusals.parkedTeardownDeletes,
		deleteJobFailures:     refusals.deleteJobFailures,
	}
	vpcPath := "/v1/resource/vpcs/" + vpcPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.record(r)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/":
			mock.handleCreate(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			mock.handleJob(w, r)
		case r.Method == http.MethodGet && r.URL.Path == vpcPath:
			mock.handleGet(w)
		case r.Method == http.MethodPut && r.URL.Path == vpcPath:
			mock.handleUpdate(w, r)
		case r.Method == http.MethodDelete && r.URL.Path == vpcPath:
			mock.handleDelete(w)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	mock.url = server.URL
	return mock
}

func (m *vpcMock) record(r *http.Request) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.requests = append(m.requests, r.Method+" "+r.URL.Path)
}

// A create puts the row back, so a GET after one answers again.
func (m *vpcMock) handleCreate(w http.ResponseWriter, r *http.Request) {
	body := testutil.ReadRequestBody(r)

	m.mutex.Lock()
	// A refused create records its body too. A second attempt is invisible in
	// the answer, because the mock refuses whatever the body carries.
	m.createBodies = append(m.createBodies, body)
	refusal := m.createRefusal
	if refusal == "" {
		m.getNotFound = false
		m.createBody = body
		m.name, _ = body["name"].(string)
		m.description, _ = body["description"].(string)
		if cidr, ok := body["cidr"].(string); ok {
			m.cidr = cidr
		}
		m.nameservers = requestedNameservers(body)
	}
	m.mutex.Unlock()

	if refusal != "" {
		writeVpcRefusal(w, http.StatusConflict, refusal)
		return
	}

	created := m.vpcBody()
	created["status"] = "creating"
	created["activeJobId"] = vpcPlanTestCreateJobID
	testutil.WriteJSONResponse(w, map[string]any{
		"success": true,
		"message": "Operation initiated successfully",
		"meta":    nil,
		"data":    map[string]any{"jobId": vpcPlanTestCreateJobID, "vpc": created},
	})
}

func (m *vpcMock) handleGet(w http.ResponseWriter) {
	m.mutex.Lock()
	notFound := m.getNotFound
	if m.tearingDown {
		m.getsWhileGoing++
		if m.deletingGets > 0 {
			m.deletingGets--
		} else {
			notFound = true
		}
	}
	m.mutex.Unlock()

	if notFound {
		writeVpcRefusal(w, http.StatusNotFound, vpcNotFoundBody)
		return
	}
	testutil.WriteJSONResponse(w, m.detailBody())
}

func (m *vpcMock) handleUpdate(w http.ResponseWriter, r *http.Request) {
	body := testutil.ReadRequestBody(r)

	_, hasName := body["name"]
	_, hasDescription := body["description"]
	if !hasName && !hasDescription {
		writeVpcRefusal(w, http.StatusUnprocessableEntity, vpcEmptyUpdateRefusalBody)
		return
	}

	m.mutex.Lock()
	if name, ok := body["name"].(string); ok {
		m.name = name
	}
	if description, ok := body["description"].(string); ok {
		m.description = description
	}
	m.mutex.Unlock()

	testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": m.vpcBody()})
}

func (m *vpcMock) handleDelete(w http.ResponseWriter) {
	m.mutex.Lock()
	tearingDown := m.tearingDownDeletes > 0
	if tearingDown {
		m.tearingDownDeletes--
		m.status = "deleting"
		m.tearingDown = true
		m.deletingGets = 2
	}
	// The dispatch dies between the refusal and the read. The row the next
	// read answers with therefore carries the parked marker.
	parked := !tearingDown && m.parkedTeardownDeletes > 0
	if parked {
		m.parkedTeardownDeletes--
		m.status = "deleting"
		m.failureReason = vpcPlanTestParkedReason
		m.tearingDown = true
		m.deletingGets = 1
	}
	refuse := !tearingDown && !parked && m.deleteRefusals > 0
	if refuse {
		m.deleteRefusals--
	}
	notFound := m.deleteNotFound
	m.mutex.Unlock()

	if tearingDown || parked {
		writeVpcRefusal(w, http.StatusConflict, vpcTearingDownRefusalBody)
		return
	}
	if refuse {
		writeVpcRefusal(w, http.StatusConflict, vpcNotEmptyRefusalBody)
		return
	}
	if notFound {
		writeVpcRefusal(w, http.StatusNotFound, vpcNotFoundBody)
		return
	}

	testutil.WriteJSONResponse(w, map[string]any{
		"success": true,
		"message": "Operation initiated successfully",
		"meta":    nil,
		"data":    map[string]any{"jobId": vpcPlanTestDeleteJobID},
	})
}

// handleJob answers the poll for the job the request names. A teardown job the
// test arms to fail answers the terminal failure shape. The row it leaves
// behind parks in deleting with the reason the job writes.
func (m *vpcMock) handleJob(w http.ResponseWriter, r *http.Request) {
	jobID := vpcPlanTestCreateJobID
	if ids, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(ids) > 0 {
		jobID, _ = ids[0].(string)
	}

	m.mutex.Lock()
	failing := jobID == vpcPlanTestDeleteJobID && m.deleteJobFailures > 0
	if failing {
		m.deleteJobFailures--
		m.tearingDown = false
		m.deletingGets = 0
		m.failureReason = vpcPlanTestAbandonedJobError
	}
	m.mutex.Unlock()

	if failing {
		testutil.WriteJSONResponse(w, map[string]any{
			"success": true,
			"message": "Job status retrieved",
			"data": map[string]any{"jobs": []map[string]any{{
				"jobId":        jobID,
				"isCompleted":  false,
				"isTerminal":   true,
				"hasFailed":    true,
				"errorMessage": vpcPlanTestAbandonedJobError,
			}}},
		})
		return
	}

	testutil.HandleJobResponse(w, jobID, vpcPlanTestID, true)
}

// egressIp stays null, so the state must keep it null too. failureReason is
// null until a teardown parks the row.
func (m *vpcMock) vpcBody() map[string]any {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return map[string]any{
		"id":              vpcPlanTestID,
		"name":            m.name,
		"description":     m.description,
		"cidr":            m.cidr,
		"datacenter":      map[string]any{"id": vpcPlanTestDatacenterID, "code": "kansas", "name": "Kansas"},
		"status":          m.status,
		"failureReason":   m.failureReason,
		"egressIp":        nil,
		"activeJobId":     nil,
		"resourceGroupId": nil,
		"createdAt":       vpcPlanTestTimestamp,
		"updatedAt":       vpcPlanTestTimestamp,
		"dnsNameservers":  m.answeredNameservers(),
	}
}

// The platform freezes the requested nameservers on the row, and resolves its
// own only when the request omits them.
func (m *vpcMock) answeredNameservers() []string {
	if len(m.nameservers) > 0 {
		return m.nameservers
	}
	return []string{"8.8.8.8", "1.1.1.1"}
}

func requestedNameservers(body map[string]any) []string {
	requested, ok := body["dnsNameservers"].([]any)
	if !ok {
		return nil
	}
	nameservers := make([]string, 0, len(requested))
	for _, entry := range requested {
		nameserver, isString := entry.(string)
		if !isString {
			continue
		}
		nameservers = append(nameservers, nameserver)
	}
	return nameservers
}

func (m *vpcMock) detailBody() map[string]any {
	data := m.vpcBody()
	data["subnetCount"] = 0
	data["nsgCount"] = 1
	data["publicIpCount"] = 0
	return map[string]any{"success": true, "message": "", "meta": nil, "data": data}
}

func (m *vpcMock) lastCreateBody() map[string]any {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.createBody
}

func (m *vpcMock) allCreateBodies() []map[string]any {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return append([]map[string]any(nil), m.createBodies...)
}

// setName renames the VPC behind the provider's back. The mutex orders the
// write against the handler goroutine that reads the field.
func (m *vpcMock) setName(name string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.name = name
}

// setGetNotFound deletes the VPC behind the provider's back. The mutex orders
// the write against the handler goroutine that reads the field.
func (m *vpcMock) setGetNotFound(notFound bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.getNotFound = notFound
}

// The count of reads the provider makes after the refusal.
func (m *vpcMock) readsWhileTearingDown() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.getsWhileGoing
}

func (m *vpcMock) requestCount(request string) int {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	count := 0
	for _, recorded := range m.requests {
		if recorded == request {
			count++
		}
	}
	return count
}

func writeVpcRefusal(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func vpcPlanTestConfig(host, name string) string {
	return vpcPlanTestConfigWith(host, fmt.Sprintf(`
  name          = %q
  datacenter_id = %q
  cidr          = %q
`, name, vpcPlanTestDatacenterID, vpcPlanTestCidr))
}

// vpcPlanTestConfigWith lets a step write the resource body itself, so one
// attribute at a time can change.
func vpcPlanTestConfigWith(host, body string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc" "test" {%s}
`, host, body)
}

// The platform answers an omitted nameserver list with its own, and the create
// 202 echoes it. A read that reconciled the echo would plan a replacement, so
// the rename step pins an in-place update.
func TestVpcResourcePlanCreatesRenamesAndDestroys(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	checkCreateBody := func(*terraform.State) error {
		body := mock.lastCreateBody()
		if body == nil {
			return fmt.Errorf("expected a create POST, got none")
		}
		if got, _ := body["cidr"].(string); got != vpcPlanTestCidr {
			return fmt.Errorf("expected create body cidr %q, got %q", vpcPlanTestCidr, got)
		}
		if _, sent := body["acknowledgeOverlap"]; sent {
			return fmt.Errorf("expected no acknowledgeOverlap in the create body, got %v", body["acknowledgeOverlap"])
		}
		if _, sent := body["dnsNameservers"]; sent {
			return fmt.Errorf("expected no dnsNameservers in the create body, got %v", body["dnsNameservers"])
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-plan-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "id", vpcPlanTestID),
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", "vpc-plan-a"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "cidr", vpcPlanTestCidr),
					resource.TestCheckResourceAttr(gpcnVpcTest, "description", ""),
					resource.TestCheckResourceAttr(gpcnVpcTest, "status", "active"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.#", "2"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.0", "8.8.8.8"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.1", "1.1.1.1"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "created_time", "Friday, 02-Jan-26 15:04:05 UTC"),
					resource.TestCheckNoResourceAttr(gpcnVpcTest, "egress_ip"),
					resource.TestCheckNoResourceAttr(gpcnVpcTest, "failure_reason"),
					resource.TestCheckNoResourceAttr(gpcnVpcTest, "active_job_id"),
					resource.TestCheckNoResourceAttr(gpcnVpcTest, "acknowledge_overlap"),
					checkCreateBody,
				),
			},
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-plan-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", "vpc-plan-b"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.#", "2"),
				),
			},
		},
	})
}

// A provider applies DNS once, at creation, and discards a later change. The
// plan therefore has to replace the VPC rather than report a change nobody
// makes.
func TestVpcResourcePlanReplacesOnDnsNameserverChange(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	withNameservers := fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc" "test" {
  name            = "vpc-dns"
  datacenter_id   = %q
  cidr            = %q
  dns_nameservers = ["9.9.9.9"]
}
`, mock.url, vpcPlanTestDatacenterID, vpcPlanTestCidr)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-dns"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.#", "2"),
				),
			},
			{
				Config: withNameservers,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.#", "1"),
					resource.TestCheckResourceAttr(gpcnVpcTest, "dns_nameservers.0", "9.9.9.9"),
				),
			},
		},
	})
}

// The overlap 409 is a confirm gate. The apply stops and names the knob that
// clears it. The provider never acknowledges the overlap by itself.
func TestVpcResourcePlanSurfacesOverlapRefusal(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{create: vpcOverlapRefusalBody})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPlanTestConfig(mock.url, "vpc-overlap"),
				ExpectError: regexp.MustCompile(`acknowledge_overlap\s+=\s+true`),
			},
		},
	})

	// The mock refuses every create, so a provider that answers the gate
	// itself would still fail the step above. Only the request log shows it.
	if count := mock.requestCount("POST /v1/resource/vpcs/"); count != 1 {
		t.Errorf("create POST count = %d, want 1", count)
	}
	for _, body := range mock.allCreateBodies() {
		if acknowledged, sent := body["acknowledgeOverlap"]; sent {
			t.Errorf("create body carries acknowledgeOverlap = %v, want the key absent", acknowledged)
		}
	}
}

// A refusal that names children is a dependency error. A retry cannot clear it,
// so the destroy fails with the census the API sent.
func TestVpcResourcePlanRefusesDeleteNotEmpty(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{deletes: 1})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-not-empty"),
			},
			{
				Config:      vpcPlanTestConfig(mock.url, "vpc-not-empty"),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`Blockers`),
			},
		},
	})
}

// A rename outside Terraform is the one drift the provider reconciles in
// place. The read has to show it, or the plan reports nothing to correct.
func TestVpcResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-named"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", "vpc-named"),
				),
			},
			{
				PreConfig: func() { mock.setName("vpc-renamed-elsewhere") },
				Config:    vpcPlanTestConfig(mock.url, "vpc-named"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", "vpc-named"),
				),
			},
		},
	})
}

// The platform allocates subnets out of the super-CIDR, and a VPC lives in one
// data center. Both are fixed for the life of the VPC.
func TestVpcResourcePlanReplacesOnImmutableAttributeChange(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	movedCidr := fmt.Sprintf(`
  name          = "vpc-cidr"
  datacenter_id = %q
  cidr          = "10.60.0.0/16"
`, vpcPlanTestDatacenterID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-cidr"),
			},
			{
				Config: vpcPlanTestConfigWith(mock.url, movedCidr),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "cidr", "10.60.0.0/16"),
				),
			},
			{
				Config: vpcPlanTestConfigWith(mock.url, `
  name          = "vpc-cidr"
  datacenter_id = "dc-2"
  cidr          = "10.60.0.0/16"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "datacenter_id", "dc-2"),
				),
			},
		},
	})
}

// The API measures the submitted name at 64 characters. A plan that accepts a
// longer one teaches the rule one failed apply at a time.
func TestVpcResourcePlanRefusesATooLongName(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPlanTestConfig(mock.url, strings.Repeat("v", 65)),
				ExpectError: regexp.MustCompile(`string length must be between 1 and 64`),
			},
		},
	})
}

// A change to a request-only attribute plans an update the API has no body
// for. The API refuses an update body that carries no updatable key, so the
// provider must send no request at all.
func TestVpcResourcePlanSendsNoEmptyUpdateBody(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	acknowledged := fmt.Sprintf(`
  name                = "vpc-ack"
  datacenter_id       = %q
  cidr                = %q
  acknowledge_overlap = true
`, vpcPlanTestDatacenterID, vpcPlanTestCidr)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-ack"),
			},
			{
				Config: vpcPlanTestConfigWith(mock.url, acknowledged),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "acknowledge_overlap", "true"),
					func(*terraform.State) error {
						if count := mock.requestCount("PUT /v1/resource/vpcs/" + vpcPlanTestID); count != 0 {
							return fmt.Errorf("expected no update request, got %d", count)
						}
						return nil
					},
				),
			},
		},
	})
}

// A VPC the platform has already removed is the state the destroy wanted.
func TestVpcResourcePlanTreatsDeleteNotFoundAsDone(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{deleteNotFound: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-gone"),
			},
			{
				Config:  vpcPlanTestConfig(mock.url, "vpc-gone"),
				Destroy: true,
			},
		},
	})
}

// GPCN trims a name and a description. A value the API would trim comes back
// different, and the plan never settles, so the plan refuses it first.
func TestVpcResourcePlanRefusesSurroundingWhitespace(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	paddedDescription := fmt.Sprintf(`
  name          = "vpc-padded"
  datacenter_id = %q
  cidr          = %q
  description   = "shared "
`, vpcPlanTestDatacenterID, vpcPlanTestCidr)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPlanTestConfig(mock.url, " vpc"),
				ExpectError: whitespaceRefusal("Invalid VPC name", "name"),
			},
			{
				Config:      vpcPlanTestConfigWith(mock.url, paddedDescription),
				ExpectError: whitespaceRefusal("Invalid VPC description", "description"),
			},
		},
	})
}

// The super-CIDR rules live in a validator the schema has to carry. A plan
// that accepts a host address teaches the rule one failed apply at a time.
func TestVpcResourcePlanRefusesACidrThatIsNotANetworkAddress(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	hostAddress := fmt.Sprintf(`
  name          = "vpc-host-address"
  datacenter_id = %q
  cidr          = "10.50.0.1/16"
`, vpcPlanTestDatacenterID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPlanTestConfigWith(mock.url, hostAddress),
				ExpectError: regexp.MustCompile(`must be a valid IPv4 CIDR whose address is the`),
			},
		},
	})
}

// A VPC deleted outside Terraform has to leave state. Every later plan
// otherwise fails on the read instead of offering to create the VPC again.
func TestVpcResourcePlanRemovesAVanishedVpcFromState(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-vanished"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "id", vpcPlanTestID),
				),
			},
			{
				PreConfig: func() { mock.setGetNotFound(true) },
				Config:    vpcPlanTestConfig(mock.url, "vpc-vanished"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "id", vpcPlanTestID),
					resource.TestCheckResourceAttr(gpcnVpcTest, "name", "vpc-vanished"),
				),
			},
		},
	})
}

// The API takes one or two distinct resolvers. A plan that accepts a third,
// or a repeat, teaches the rule one failed apply at a time.
func TestVpcResourcePlanRefusesAnIllegalNameserverList(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{})

	withNameservers := func(nameservers string) string {
		return vpcPlanTestConfigWith(mock.url, fmt.Sprintf(`
  name            = "vpc-dns-rules"
  datacenter_id   = %q
  cidr            = %q
  dns_nameservers = %s
`, vpcPlanTestDatacenterID, vpcPlanTestCidr, nameservers))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      withNameservers(`["9.9.9.9", "8.8.8.8", "1.1.1.1"]`),
				ExpectError: regexp.MustCompile(`at most 2\s+elements`),
			},
			{
				Config:      withNameservers(`["9.9.9.9", "9.9.9.9"]`),
				ExpectError: regexp.MustCompile(`contains duplicate values of`),
			},
			// Terraform hard-wraps a diagnostic, so the count and the noun can
			// land on separate lines.
			{
				Config:      withNameservers(`[]`),
				ExpectError: regexp.MustCompile(`at least 1\s+elements`),
			},
		},
	})
}

// A teardown another caller started refuses the second claim, and the refusal
// names the very work the destroy asked for. The provider waits for the row to
// go instead of handing the reader an error about its own request.
func TestVpcResourcePlanWaitsOutATeardownAlreadyRunning(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{tearingDownDeletes: 1})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-tearing-down"),
			},
			{
				Config:  vpcPlanTestConfig(mock.url, "vpc-tearing-down"),
				Destroy: true,
			},
		},
	})

	// The status read plus the two polls that end at the 404.
	if count := mock.readsWhileTearingDown(); count != 3 {
		t.Errorf("reads while tearing down = %d, want 3", count)
	}
}

// A teardown whose dispatch fails parks the row in deleting with a reason.
// No job then drives that row, so a wait for the 404 never ends. The platform
// re-admits a delete on a parked row, and the provider claims it again.
func TestVpcResourcePlanReclaimsAParkedTeardown(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{parkedTeardownDeletes: 1})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPlanTestConfig(mock.url, "vpc-parked-teardown"),
			},
			{
				Config:  vpcPlanTestConfig(mock.url, "vpc-parked-teardown"),
				Destroy: true,
			},
		},
	})

	if count := mock.requestCount("DELETE /v1/resource/vpcs/" + vpcPlanTestID); count != 2 {
		t.Errorf("DELETE count = %d, want 2", count)
	}
	// The status read alone. A provider that waits polls the row as well.
	if count := mock.readsWhileTearingDown(); count != 1 {
		t.Errorf("reads while tearing down = %d, want 1", count)
	}
}

// A teardown job that fails leaves the VPC in place. The provider reports the
// job error instead of a destroy it never achieves. The row stays in state for
// the next apply to claim again.
func TestVpcResourcePlanReportsAParkedReclaimWhoseJobFails(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t, vpcMockRefusals{parkedTeardownDeletes: 1, deleteJobFailures: 1})
	config := vpcPlanTestConfig(mock.url, "vpc-reclaim-job-fails")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				Config:      config,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)job\s+operation\s+failed.*never\s+reached\s+a\s+terminal\s+stage`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcTest, "id", vpcPlanTestID),
				),
			},
		},
	})
}
