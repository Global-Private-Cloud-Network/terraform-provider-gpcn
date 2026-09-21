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

// vpcMock serves the VPC endpoints an apply walks. It keeps the name the last
// create or update sent. The read after an apply then agrees with the
// configuration, and the refresh plan stays empty.
type vpcMock struct {
	url string

	mutex          sync.Mutex
	name           string
	description    string
	nameservers    []string
	createRefusal  string
	deleteRefusals int
	createBody     map[string]any
	requests       []string
}

func startVpcPlanMockServer(t *testing.T) *vpcMock {
	t.Helper()

	mock := &vpcMock{}
	vpcPath := "/v1/resource/vpcs/" + vpcPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.record(r)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/":
			mock.handleCreate(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, vpcPlanTestCreateJobID, vpcPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vpcPath:
			testutil.WriteJSONResponse(w, mock.detailBody())
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

func (m *vpcMock) handleCreate(w http.ResponseWriter, r *http.Request) {
	body := testutil.ReadRequestBody(r)

	m.mutex.Lock()
	refusal := m.createRefusal
	if refusal == "" {
		m.createBody = body
		m.name, _ = body["name"].(string)
		m.description, _ = body["description"].(string)
		m.nameservers = requestedNameservers(body)
	}
	m.mutex.Unlock()

	if refusal != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(refusal))
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

func (m *vpcMock) handleUpdate(w http.ResponseWriter, r *http.Request) {
	body := testutil.ReadRequestBody(r)

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
	refuse := m.deleteRefusals > 0
	if refuse {
		m.deleteRefusals--
	}
	m.mutex.Unlock()

	if refuse {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(vpcNotEmptyRefusalBody))
		return
	}

	testutil.WriteJSONResponse(w, map[string]any{
		"success": true,
		"message": "Operation initiated successfully",
		"meta":    nil,
		"data":    map[string]any{"jobId": vpcPlanTestDeleteJobID},
	})
}

// egressIp and failureReason stay null, so the state must keep them null too.
func (m *vpcMock) vpcBody() map[string]any {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return map[string]any{
		"id":              vpcPlanTestID,
		"name":            m.name,
		"description":     m.description,
		"cidr":            vpcPlanTestCidr,
		"datacenter":      map[string]any{"id": vpcPlanTestDatacenterID, "code": "kansas", "name": "Kansas"},
		"status":          "active",
		"failureReason":   nil,
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

func vpcPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc" "test" {
  name          = %q
  datacenter_id = %q
  cidr          = %q
}
`, host, name, vpcPlanTestDatacenterID, vpcPlanTestCidr)
}

// The platform answers an omitted nameserver list with its own, and the create
// 202 echoes it. A read that reconciled the echo would plan a replacement, so
// the rename step pins an in-place update.
func TestVpcResourcePlanCreatesRenamesAndDestroys(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t)

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
	mock := startVpcPlanMockServer(t)

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
	mock := startVpcPlanMockServer(t)
	mock.createRefusal = vpcOverlapRefusalBody

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPlanTestConfig(mock.url, "vpc-overlap"),
				ExpectError: regexp.MustCompile(`acknowledge_overlap\s+=\s+true`),
			},
		},
	})
}

// A refusal that names children is a dependency error. A retry cannot clear it,
// so the destroy fails with the census the API sent.
func TestVpcResourcePlanRefusesDeleteNotEmpty(t *testing.T) {
	t.Parallel()
	mock := startVpcPlanMockServer(t)
	mock.deleteRefusals = 1

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
